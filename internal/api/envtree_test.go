package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tree builds a storage layout for the layered environments: a base file at
// the root and one env.json per project folder under requests/. The map is
// keyed by the scope, with "" meaning the base file.
func tree(t *testing.T, files map[string]string) (base, root string) {
	t.Helper()
	dir := t.TempDir()
	base = filepath.Join(dir, "env.json")
	root = filepath.Join(dir, "requests")

	for scope, body := range files {
		path := base
		if scope != "" {
			folder := filepath.Join(root, filepath.FromSlash(scope))
			if err := os.MkdirAll(folder, 0o700); err != nil {
				t.Fatal(err)
			}
			path = filepath.Join(folder, "env.json")
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return base, root
}

func loadTree(t *testing.T, files map[string]string) *Env {
	t.Helper()
	base, root := tree(t, files)
	e, err := LoadEnvTree(base, root)
	if err != nil {
		t.Fatalf("LoadEnvTree: %v", err)
	}
	return e
}

// twoProjects is the layout the whole feature exists for: four stages, two
// projects, and a shared value that lives in one place rather than eight.
var twoProjects = map[string]string{
	"": `{
  "local": { "team": "platform" },
  "sit":   { "team": "platform" },
  "uat":   { "team": "platform" },
  "prod":  { "team": "platform" }
}`,
	"gtos": `{
  "local": { "base": "http://localhost:8080" },
  "sit":   { "base": "https://gtos.sit"  },
  "uat":   { "base": "https://gtos.uat"  },
  "prod":  { "base": "https://gtos.corp" }
}`,
	"payments": `{
  "local": { "base": "http://localhost:9090" },
  "sit":   { "base": "https://pay.sit"  },
  "uat":   { "base": "https://pay.uat"  },
  "prod":  { "base": "https://pay.corp" }
}`,
}

// TestCycleLengthIsStagesNotTheProduct is the point of the design. Two projects
// with four stages each must give a four-entry cycle, not eight -- and adding a
// third project must not make it any longer.
func TestCycleLengthIsStagesNotTheProduct(t *testing.T) {
	e := loadTree(t, twoProjects)

	e.SetScope("gtos")
	got := e.Names()
	want := []string{"local", "sit", "uat", "prod"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Names() in gtos = %v, want %v", got, want)
	}

	e.SetScope("payments")
	if got := e.Names(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Names() in payments = %v, want %v", got, want)
	}
}

// TestStageIsGlobalAndValuesArePerProject: Ctrl+E once, and every project is on
// UAT -- each resolving to its own host.
func TestStageIsGlobalAndValuesArePerProject(t *testing.T) {
	e := loadTree(t, twoProjects)
	e.SetActive("uat")

	e.SetScope("gtos")
	if got, _ := e.Expand("{{base}}"); got != "https://gtos.uat" {
		t.Errorf("gtos base = %q", got)
	}

	e.SetScope("payments")
	if got, _ := e.Expand("{{base}}"); got != "https://pay.uat" {
		t.Errorf("payments base = %q", got)
	}

	// The stage did not change when the project did. That is what makes one
	// Ctrl+E mean "put the app on UAT".
	if e.Active() != "uat" {
		t.Errorf("Active() = %q after changing project, want uat", e.Active())
	}
}

// TestBaseIsALayerNotAFallbackFile: a value the projects share is written once
// at the root and resolves inside either of them.
func TestBaseIsALayerNotAFallbackFile(t *testing.T) {
	e := loadTree(t, twoProjects)
	e.SetActive("sit")

	for _, scope := range []string{"gtos", "payments"} {
		e.SetScope(scope)
		got, missing := e.Expand("{{team}}/{{base}}")
		if len(missing) > 0 {
			t.Fatalf("%s: missing %v", scope, missing)
		}
		if !strings.HasPrefix(got, "platform/") {
			t.Errorf("%s: %q does not carry the shared value", scope, got)
		}
	}
}

// TestProjectOverridesTheBase. Both layers naming the same key is the ordinary
// case -- the project is the specific one, so it wins.
func TestProjectOverridesTheBase(t *testing.T) {
	e := loadTree(t, map[string]string{
		"":     `{ "uat": { "base": "https://shared", "team": "platform" } }`,
		"gtos": `{ "uat": { "base": "https://gtos.uat" } }`,
	})
	e.SetActive("uat")
	e.SetScope("gtos")

	if got, _ := e.Expand("{{base}}"); got != "https://gtos.uat" {
		t.Errorf("base = %q, want the project's", got)
	}
	if got, _ := e.Expand("{{team}}"); got != "platform" {
		t.Errorf("team = %q, want the base's", got)
	}
}

// TestScopeWalksUpToTheProject: a request in a subfolder resolves against the
// project above it, and is treated as belonging to that project.
func TestScopeWalksUpToTheProject(t *testing.T) {
	e := loadTree(t, map[string]string{
		"gtos": `{ "uat": { "base": "https://gtos.uat" } }`,
	})
	e.SetActive("uat")
	e.SetScope("gtos/admin/reports")

	if got, _ := e.Expand("{{base}}"); got != "https://gtos.uat" {
		t.Errorf("base = %q, want the project's", got)
	}
	if e.Project() != "gtos" {
		t.Errorf("Project() = %q, want gtos", e.Project())
	}
}

// TestNestedProjectWins: a folder with its own file is its own project, even
// inside one that has a file too, and the outer file still layers underneath.
func TestNestedProjectWins(t *testing.T) {
	e := loadTree(t, map[string]string{
		"gtos":       `{ "uat": { "base": "https://gtos.uat", "team": "platform" } }`,
		"gtos/batch": `{ "uat": { "base": "https://batch.uat" } }`,
	})
	e.SetActive("uat")
	e.SetScope("gtos/batch")

	if got, _ := e.Expand("{{base}}"); got != "https://batch.uat" {
		t.Errorf("base = %q, want the nested project's", got)
	}
	if got, _ := e.Expand("{{team}}"); got != "platform" {
		t.Errorf("team = %q, want the enclosing project's", got)
	}
	if e.Project() != "gtos/batch" {
		t.Errorf("Project() = %q", e.Project())
	}
}

// TestOverlayIsScopedToTheProject is the safety property one level down from
// the per-stage one: a token a hook lifted out of gtos on uat must not be in
// scope for payments on uat. That would be a request sent to one system with
// another system's credential.
func TestOverlayIsScopedToTheProject(t *testing.T) {
	e := loadTree(t, twoProjects)
	e.SetActive("uat")

	e.SetScope("gtos")
	e.Set("token", "gtos-token")

	e.SetScope("payments")
	if v, ok := e.Lookup("token"); ok {
		t.Errorf("payments sees %q; the gtos token must not be in scope", v)
	}

	e.SetScope("gtos")
	if v, _ := e.Lookup("token"); v != "gtos-token" {
		t.Errorf("gtos lost its own token: %q", v)
	}
}

// TestOverlayIsStillScopedToTheStage. The project scoping is in addition to
// the per-stage scoping, not instead of it.
func TestOverlayIsStillScopedToTheStage(t *testing.T) {
	e := loadTree(t, twoProjects)
	e.SetScope("gtos")
	e.SetActive("uat")
	e.Set("token", "uat-token")

	e.SetActive("prod")
	if v, ok := e.Lookup("token"); ok {
		t.Errorf("prod sees %q; the uat token must not be in scope", v)
	}
}

// TestDefinedReportsAStageAProjectDoesNotHave. The stage is global, so it can
// be pointed at a project that has never heard of it -- which has to be visible
// rather than looking like broken substitution.
func TestDefinedReportsAStageAProjectDoesNotHave(t *testing.T) {
	e := loadTree(t, map[string]string{
		"gtos":     `{ "uat": { "base": "https://gtos.uat" }, "prod": { "base": "https://gtos.corp" } }`,
		"payments": `{ "uat": { "base": "https://pay.uat" } }`,
	})
	e.SetActive("prod")

	e.SetScope("gtos")
	if !e.Defined() {
		t.Error("gtos defines prod; Defined() said otherwise")
	}

	e.SetScope("payments")
	if e.Defined() {
		t.Error("payments has no prod; Defined() must say so")
	}
}

// TestSetActiveAcceptsAStageOnlyAnotherProjectDefines. Refusing it would make
// the selection depend on where the highlight happened to be, which is exactly
// the per-project coupling this design removes.
func TestSetActiveAcceptsAStageOnlyAnotherProjectDefines(t *testing.T) {
	e := loadTree(t, map[string]string{
		"gtos":     `{ "uat": {}, "prod": {} }`,
		"payments": `{ "uat": {} }`,
	})
	e.SetScope("payments")
	e.SetActive("prod")
	if e.Active() != "prod" {
		t.Errorf("Active() = %q, want prod", e.Active())
	}
}

// TestEditPathFindsTheProjectFile is what Alt+E opens.
func TestEditPathFindsTheProjectFile(t *testing.T) {
	base, root := tree(t, twoProjects)
	e, err := LoadEnvTree(base, root)
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(root, "gtos", "env.json")
	if got := e.EditPath("gtos/admin"); got != want {
		t.Errorf("EditPath(gtos/admin) = %q, want %q", got, want)
	}
	if got := e.EditPath(""); got != base {
		t.Errorf("EditPath(root) = %q, want the base file %q", got, base)
	}
}

// TestEditPathOffersTheProjectsOwnFile: a project with no env.json yet must be
// offered one of its own, not sent to the shared file -- otherwise the first
// edit in a new project quietly writes into everybody else's.
func TestEditPathOffersTheProjectsOwnFile(t *testing.T) {
	base, root := tree(t, map[string]string{"": `{ "uat": {} }`})
	e, err := LoadEnvTree(base, root)
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(root, "newproject", "env.json")
	if got := e.EditPath("newproject"); got != want {
		t.Errorf("EditPath = %q, want %q", got, want)
	}
}

// TestABrokenProjectFileDoesNotStopTheOthers. One typo must cost one project,
// not the tab -- and it has to be named, or it looks like the substitution
// silently breaking.
func TestABrokenProjectFileDoesNotStopTheOthers(t *testing.T) {
	base, root := tree(t, map[string]string{
		"":         `{ "uat": { "team": "platform" } }`,
		"gtos":     `{ "uat": { "base": "https://gtos.uat" } }`,
		"payments": `{ "uat": { "base": `, // truncated mid-value
	})

	e, err := LoadEnvTree(base, root)
	if err == nil {
		t.Fatal("LoadEnvTree: want an error naming the broken file")
	}
	if !strings.Contains(err.Error(), "payments") {
		t.Errorf("error %q does not name the broken file", err)
	}
	if e == nil {
		t.Fatal("LoadEnvTree returned no Env; a broken file must not cost the good ones")
	}

	e.SetActive("uat")
	e.SetScope("gtos")
	if got, _ := e.Expand("{{base}}"); got != "https://gtos.uat" {
		t.Errorf("gtos base = %q; a broken sibling must not affect it", got)
	}
}

// TestAdoptKeepsTheStageAndTheProjectAcrossAReload. Saving a file makes the
// watcher reload the whole tree, and the token just fetched -- and the stage
// just selected -- have to survive it.
func TestAdoptKeepsTheStageAndTheProjectAcrossAReload(t *testing.T) {
	base, root := tree(t, twoProjects)

	old, err := LoadEnvTree(base, root)
	if err != nil {
		t.Fatal(err)
	}
	old.SetActive("prod")
	old.SetScope("payments")
	old.Set("token", "carried")

	fresh, err := LoadEnvTree(base, root)
	if err != nil {
		t.Fatal(err)
	}
	fresh.Adopt(old)

	if fresh.Active() != "prod" {
		t.Errorf("Active() = %q, want prod", fresh.Active())
	}
	if fresh.Project() != "payments" {
		t.Errorf("Project() = %q, want payments", fresh.Project())
	}
	if v, _ := fresh.Lookup("token"); v != "carried" {
		t.Errorf("token = %q, want it carried across the reload", v)
	}
	if got, _ := fresh.Expand("{{base}}"); got != "https://pay.corp" {
		t.Errorf("base = %q, want the payments prod host", got)
	}
}

// TestNoFilesAtAllStillWorks. The tab is usable with no environments: every
// {{var}} simply stays as written.
func TestNoFilesAtAllStillWorks(t *testing.T) {
	dir := t.TempDir()
	e, err := LoadEnvTree(filepath.Join(dir, "env.json"), filepath.Join(dir, "requests"))
	if err != nil {
		t.Fatalf("LoadEnvTree: %v", err)
	}
	e.SetScope("gtos")
	got, missing := e.Expand("{{base}}/orders")
	if got != "{{base}}/orders" {
		t.Errorf("Expand = %q, want it left alone", got)
	}
	if len(missing) != 1 || missing[0] != "base" {
		t.Errorf("missing = %v, want [base]", missing)
	}
}

// TestUnderscoreKeysAreComments. The checked-in template leads with one
// explaining what the file is for, and before this it was read as a stage whose
// body was a string -- so copying storage.example gave an env.json that did not
// parse, which is the first thing a new user would have seen.
func TestUnderscoreKeysAreComments(t *testing.T) {
	e := load(t, `{
  "_comment": "what this file is for",
  "_notes":   { "anything": "at all" },
  "uat":      { "base": "https://api.uat" }
}`)

	if got := e.Names(); len(got) != 1 || got[0] != "uat" {
		t.Fatalf("Names() = %v, want [uat] -- the comments are not stages", got)
	}
	e.SetActive("uat")
	if got, _ := e.Expand("{{base}}"); got != "https://api.uat" {
		t.Errorf("base = %q", got)
	}
}

// TestTheCheckedInTemplateParses. It is the first file anyone uses, and it
// shipped broken once already.
func TestTheCheckedInTemplateParses(t *testing.T) {
	e, err := LoadEnv(filepath.Join("..", "..", "storage.example", "env.json"))
	if err != nil {
		t.Fatalf("storage.example/env.json: %v", err)
	}
	if len(e.Names()) == 0 {
		t.Error("the template defines no stages")
	}
}
