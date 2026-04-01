package commands

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/jfrog/jfrog-cli-core/v2/plugins/components"
	"github.com/jfrog/jfrog-cli-core/v2/utils/config"
	"github.com/jfrog/jfrog-client-go/artifactory"
	clientConfig "github.com/jfrog/jfrog-client-go/config"
	"github.com/jfrog/jfrog-client-go/utils/log"
)

var safeNamePattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// ── Types ───────────────────────────────────────────────────────────────────

type buildInfo struct {
	Name    string
	Number  string
	Created string
}

type releaseBundleRequest struct {
	Name                         string   `json:"release_bundle_name"`
	Version                      string   `json:"release_bundle_version"`
	KeypairName                  string   `json:"keypair_name,omitempty"`
	SkipDockerManifestResolution bool     `json:"skip_docker_manifest_resolution"`
	SourceType                   string   `json:"source_type"`
	Source                       rbSource `json:"source"`
}

type rbSource struct {
	Builds []rbBuild `json:"builds"`
}

type rbBuild struct {
	BuildName           string `json:"build_name"`
	BuildNumber         string `json:"build_number"`
	IncludeDependencies bool   `json:"include_dependencies"`
}

// ── Command Definition ──────────────────────────────────────────────────────

func GetCreateCommand() components.Command {
	return components.Command{
		Name:        "create",
		Description: "Create a Release Bundle v2 from the latest builds matching an environment filter.",
		Aliases:     []string{"c"},
		Arguments:   getCreateArguments(),
		Flags:       getCreateFlags(),
		Action: func(c *components.Context) error {
			return createAction(c)
		},
	}
}

func getCreateArguments() []components.Argument {
	return []components.Argument{
		{
			Name:        "build-names",
			Description: "One or more build names to include in the Release Bundle.",
		},
	}
}

func getCreateFlags() []components.Flag {
	return []components.Flag{
		components.NewStringFlag(
			"env-key", "Build property key to filter on.",
			components.WithStrDefaultValue("buildInfo.env.GITHUB_REPOSITORY_OWNER"),
		),
		components.NewStringFlag(
			"env-value", "Build property value to match.",
			components.WithStrDefaultValue("chaitanyagovande"),
		),
		components.NewStringFlag(
			"rb-name", "Release Bundle name.",
			components.WithStrDefaultValue("my-release-bundle"),
		),
		components.NewStringFlag(
			"rb-version", "Release Bundle version.",
			components.WithStrDefaultValue("1.0.0"),
		),
		components.NewStringFlag(
			"signing-key", "GPG signing key name.",
		),
		components.NewStringFlag(
			"project", "JFrog project key.",
		),
		components.NewStringFlag(
			"server-id", "JFrog CLI server configuration ID.",
		),
		components.NewBoolFlag(
			"include-deps", "Include build dependencies in the Release Bundle.",
			components.WithBoolDefaultValue(false),
		),
		components.NewBoolFlag(
			"dry-run", "Print the payload without creating the Release Bundle.",
			components.WithBoolDefaultValue(false),
		),
		components.NewBoolFlag(
			"fail-on-missing", "Fail if any requested build name is not found.",
			components.WithBoolDefaultValue(true),
		),
	}
}

// ── Action ──────────────────────────────────────────────────────────────────

func createAction(c *components.Context) error {
	if len(c.Arguments) < 1 {
		return fmt.Errorf("at least one build name argument is required")
	}

	buildNames := c.Arguments
	envKey := c.GetStringFlagValue("env-key")
	envValue := c.GetStringFlagValue("env-value")
	rbName := c.GetStringFlagValue("rb-name")
	rbVersion := c.GetStringFlagValue("rb-version")
	signingKey := c.GetStringFlagValue("signing-key")
	project := c.GetStringFlagValue("project")
	serverID := c.GetStringFlagValue("server-id")
	includeDeps := c.GetBoolFlagValue("include-deps")
	dryRun := c.GetBoolFlagValue("dry-run")
	failOnMissing := c.GetBoolFlagValue("fail-on-missing")

	if err := validateInputs(buildNames, rbName, rbVersion, signingKey, project); err != nil {
		return err
	}

	serverDetails, err := getServerDetails(serverID)
	if err != nil {
		return fmt.Errorf("server configuration: %w", err)
	}

	rtManager, err := createRtManager(serverDetails)
	if err != nil {
		return fmt.Errorf("Artifactory manager: %w", err)
	}

	// Step 1: AQL query
	log.Info("============================================================")
	log.Info(fmt.Sprintf(" Release Bundle : %s v%s", rbName, rbVersion))
	log.Info(fmt.Sprintf(" Filter         : %s = %s", envKey, envValue))
	log.Info(fmt.Sprintf(" Builds         : %s", strings.Join(buildNames, ", ")))
	log.Info("============================================================")
	log.Info("[1/4] Running AQL query...")

	aqlQuery := buildAQLQuery(buildNames, envKey, envValue)
	results, total, err := runAQL(rtManager, aqlQuery)
	if err != nil {
		return fmt.Errorf("AQL query: %w", err)
	}
	log.Info(fmt.Sprintf("Found %d total matching build(s).", total))
	if total == 0 {
		return fmt.Errorf("no builds found matching the criteria")
	}

	// Step 2: extract latest build per name
	log.Info("[2/4] Extracting latest build per name...")
	latest := extractLatestBuilds(results)
	if len(latest) == 0 {
		return fmt.Errorf("failed to extract build info — unexpected AQL response field names")
	}
	for _, b := range latest {
		log.Info(fmt.Sprintf("  %s #%s (created: %s)", b.Name, b.Number, b.Created))
	}

	missing := findMissing(buildNames, latest)
	if len(missing) > 0 {
		log.Warn(fmt.Sprintf("Build name(s) not found: %s", strings.Join(missing, ", ")))
		if failOnMissing {
			return fmt.Errorf("missing builds: %s (use --fail-on-missing=false to continue)", strings.Join(missing, ", "))
		}
		log.Warn("Continuing without missing builds.")
	}

	// Step 3: build payload
	log.Info("[3/4] Constructing Release Bundle payload...")
	payload := buildPayload(rbName, rbVersion, signingKey, latest, includeDeps)
	payloadJSON, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	log.Output(string(payloadJSON))

	if dryRun {
		log.Info("[4/4] DRY RUN — skipping Release Bundle creation.")
		return nil
	}

	// Step 4: create release bundle
	log.Info("[4/4] Creating Release Bundle v2...")
	if err := postReleaseBundle(serverDetails, payloadJSON, project); err != nil {
		return fmt.Errorf("Release Bundle creation: %w", err)
	}

	log.Info("============================================================")
	log.Info(fmt.Sprintf(" Release Bundle created: %s v%s", rbName, rbVersion))
	log.Info("============================================================")
	return nil
}

// ── Validation ──────────────────────────────────────────────────────────────

func validateInputs(buildNames []string, rbName, rbVersion, signingKey, project string) error {
	for _, name := range buildNames {
		if !safeNamePattern.MatchString(name) {
			return fmt.Errorf("build name %q contains invalid characters (allowed: a-zA-Z0-9._-)", name)
		}
	}
	if !safeNamePattern.MatchString(rbName) {
		return fmt.Errorf("--rb-name %q contains invalid characters", rbName)
	}
	if !safeNamePattern.MatchString(rbVersion) {
		return fmt.Errorf("--rb-version %q contains invalid characters", rbVersion)
	}
	if signingKey != "" && !safeNamePattern.MatchString(signingKey) {
		return fmt.Errorf("--signing-key %q contains invalid characters", signingKey)
	}
	if project != "" && !safeNamePattern.MatchString(project) {
		return fmt.Errorf("--project %q contains invalid characters", project)
	}
	return nil
}

// ── Server & Artifactory ────────────────────────────────────────────────────

func getServerDetails(serverID string) (*config.ServerDetails, error) {
	if serverID != "" {
		return config.GetSpecificConfig(serverID, true, true)
	}
	return config.GetDefaultServerConf()
}

func createRtManager(sd *config.ServerDetails) (artifactory.ArtifactoryServicesManager, error) {
	artAuth, err := sd.CreateArtAuthConfig()
	if err != nil {
		return nil, err
	}
	artAuth.SetUrl(sd.GetArtifactoryUrl())
	cfg, err := clientConfig.NewConfigBuilder().
		SetServiceDetails(artAuth).
		Build()
	if err != nil {
		return nil, err
	}
	return artifactory.New(cfg)
}

// ── AQL ─────────────────────────────────────────────────────────────────────

func buildAQLQuery(buildNames []string, envKey, envValue string) string {
	orClauses := make([]string, len(buildNames))
	for i, name := range buildNames {
		orClauses[i] = fmt.Sprintf(`{"name": "%s"}`, name)
	}
	return fmt.Sprintf(
		`builds.find({"$and": [{"property.key": "%s"}, {"property.value": "%s"}, {"$or": [%s]}]}).include("name","number","created").sort({"$desc": ["created"]}).limit(100000)`,
		envKey, envValue, strings.Join(orClauses, ","),
	)
}

func runAQL(mgr artifactory.ArtifactoryServicesManager, query string) ([]map[string]interface{}, int, error) {
	reader, err := mgr.Aql(query)
	if err != nil {
		return nil, 0, err
	}
	defer reader.Close()

	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, 0, err
	}

	var resp struct {
		Results []map[string]interface{} `json:"results"`
		Range   struct {
			Total int `json:"total"`
		} `json:"range"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, 0, fmt.Errorf("parse response: %w (first 200 chars: %s)", err, truncate(string(body), 200))
	}
	return resp.Results, resp.Range.Total, nil
}

// ── Build Extraction ────────────────────────────────────────────────────────

// extractLatestBuilds normalises AQL field names (handles both "build.name"
// and "name" conventions) then returns the most recently created build per
// unique build name.
func extractLatestBuilds(results []map[string]interface{}) []buildInfo {
	builds := make([]buildInfo, 0, len(results))
	for _, r := range results {
		b := parseBuild(r)
		if b.Name != "" {
			builds = append(builds, b)
		}
	}

	grouped := map[string][]buildInfo{}
	for _, b := range builds {
		grouped[b.Name] = append(grouped[b.Name], b)
	}

	latest := make([]buildInfo, 0, len(grouped))
	for _, group := range grouped {
		sort.Slice(group, func(i, j int) bool {
			return group[i].Created > group[j].Created
		})
		latest = append(latest, group[0])
	}
	sort.Slice(latest, func(i, j int) bool {
		return latest[i].Name < latest[j].Name
	})
	return latest
}

func parseBuild(raw map[string]interface{}) buildInfo {
	return buildInfo{
		Name:    coalesce(raw, "build.name", "name"),
		Number:  coalesce(raw, "build.number", "number"),
		Created: coalesce(raw, "build.created", "created"),
	}
}

// coalesce returns the string value of the first key found in the map.
func coalesce(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

func findMissing(requested []string, found []buildInfo) []string {
	have := map[string]bool{}
	for _, b := range found {
		have[b.Name] = true
	}
	var missing []string
	for _, n := range requested {
		if !have[n] {
			missing = append(missing, n)
		}
	}
	return missing
}

// ── Release Bundle Creation ─────────────────────────────────────────────────

func buildPayload(rbName, rbVersion, signingKey string, builds []buildInfo, includeDeps bool) releaseBundleRequest {
	rbBuilds := make([]rbBuild, len(builds))
	for i, b := range builds {
		rbBuilds[i] = rbBuild{
			BuildName:           b.Name,
			BuildNumber:         b.Number,
			IncludeDependencies: includeDeps,
		}
	}
	return releaseBundleRequest{
		Name:        rbName,
		Version:     rbVersion,
		KeypairName: signingKey,
		SourceType:  "builds",
		Source:      rbSource{Builds: rbBuilds},
	}
}

func postReleaseBundle(sd *config.ServerDetails, payload []byte, project string) error {
	baseURL := strings.TrimSuffix(sd.GetUrl(), "/")
	if baseURL == "" {
		baseURL = strings.TrimSuffix(sd.GetArtifactoryUrl(), "/")
	}
	if baseURL == "" {
		return fmt.Errorf("no platform or Artifactory URL configured (run: jf config add)")
	}
	endpoint := baseURL + "/lifecycle/api/v2/release_bundle"
	if project != "" {
		endpoint += "?project=" + project
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	token := sd.GetAccessToken()
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else if sd.GetUser() != "" {
		req.SetBasicAuth(sd.GetUser(), sd.GetPassword())
	} else {
		return fmt.Errorf("no authentication credentials found in server config")
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: sd.InsecureTls},
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 500))
	}

	var parsed map[string]interface{}
	if json.Unmarshal(body, &parsed) == nil {
		if _, hasErrors := parsed["errors"]; hasErrors {
			return fmt.Errorf("API error: %s", string(body))
		}
	}

	log.Output(string(body))
	return nil
}

// ── Util ────────────────────────────────────────────────────────────────────

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
