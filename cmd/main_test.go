package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
	"gopkg.in/yaml.v3"

	"github.com/matthewhughes934/go-changed-pkgs/internal/git"
)

var (
	// args to be copied for tests, just contains a placeholder for os.Args[0].
	progArgs = []string{"prog-name"}

	// path to the test module.
	modPath = filepath.Join("cmd", "testdata", "repo")

	// mutex to avoid concurrent `git worktree` commands.
	worktreeMutex sync.Mutex
)

func isWindows() bool {
	return runtime.GOOS == "windows"
}

// the name of the test module.
const testModuleName = "example.com/test-repo"

// map of patch name -> expected changed packages.
type testCfg map[string][]string

// some of these tests run based off testdata containing:
//
//   - a test module with some basic Go files
//   - a set of patches to be applied to the files in this module,
//     and the expected changed outout
//
// load these configs from: testdata/repo/patches/config.yaml.
func loadTestConfigs(t *testing.T) testCfg {
	t.Helper()
	configPath := filepath.Join(getPatchesPath(t), "config.yaml")
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)

	configs := make(testCfg)
	require.NoError(t, yaml.Unmarshal(data, &configs))

	for _, expectedRelPkgs := range configs {
		// for simplicity, config.yaml just contains relative package paths,
		// so add back the module name to these to get complete package paths
		for i, pkg := range expectedRelPkgs {
			expectedRelPkgs[i] = testModuleName + pkg
		}
	}

	return configs
}

// run tests in a new worktree to avoid polluting the current repo,
// also lets us parallelise.
func setupWorktree(t *testing.T, name string) string {
	t.Helper()
	worktreePath := filepath.Join(t.TempDir(), name)

	worktreeMutex.Lock()
	defer worktreeMutex.Unlock()
	// --detach to avoid creating a new branch in the current repo
	mustRunGitCmd(t, "worktree", "add", "--quiet", "--detach", worktreePath)
	t.Cleanup(func() {
		// try to cleanup the worktree, just to be polite
		// but the TempDir should be deleted at the end of the test run regardless
		if _, err := git.RunGitCmd(context.Background(), "worktree", "remove", "--force", worktreePath); err != nil {
			t.Logf(
				"failed to remove worktree at %s (you may want to manually remove it): %v",
				worktreePath,
				err,
			)
		}
	})

	return worktreePath
}

func getPatchesPath(t *testing.T) string {
	t.Helper()
	patchesPath, err := filepath.Abs(filepath.Join("testdata", "repo", "patches"))
	require.NoError(t, err)
	return patchesPath
}

func runWithPatches(t *testing.T, worktreePath string, patchNames []string, buf io.Writer) error {
	t.Helper()
	modDir := filepath.Join(worktreePath, modPath)

	prePatchHead, postPatchHead := commitPatches(t, worktreePath, patchNames...)

	args := append( //nolint:gocritic
		progArgs,
		"--repo-dir",
		worktreePath,
		"--mod-dir",
		modDir,
		"--from-ref",
		prePatchHead,
		"--to-ref",
		postPatchHead,
	)
	app := buildTestApp(buf)
	_, err := runApp(context.Background(), app, args)
	return err
}

func TestWithSingleCommit(t *testing.T) {
	t.Parallel()
	configs := loadTestConfigs(t)

	for name, expected := range configs {
		worktreeName := "patch-test-" + name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer

			err := runWithPatches(t, setupWorktree(t, worktreeName), []string{name}, &buf)

			require.NoError(t, err)
			compareResults(t, expected, buf)
		})
	}
}

func TestToShaNotHead(t *testing.T) {
	t.Parallel()
	configs := loadTestConfigs(t)

	// for simplicity: patches with non-intersecting expected changed files
	patchesForCommit := []string{
		"change-in-top-level-package.patch",
		"change-in-embedded-file.patch",
	}

	expected := []string{}
	for _, name := range patchesForCommit {
		expected = append(expected, configs[name]...)
	}

	worktreeName := "changed-test-many-commits"
	worktreePath := setupWorktree(t, worktreeName)
	modDir := filepath.Join(worktreePath, modPath)
	prePatchHead, postPatchHead := commitPatches(t, worktreePath, patchesForCommit...)

	// add extra commit so HEAD != toSHA
	commitPatches(t, worktreePath, "upgrade-top-level-dependency.patch")

	var buf bytes.Buffer
	args := append( //nolint:gocritic
		progArgs,
		"--repo-dir",
		worktreePath,
		"--mod-dir",
		modDir,
		"--from-ref",
		prePatchHead,
		"--to-ref",
		postPatchHead,
	)
	app := buildTestApp(&buf)
	retCode, err := runApp(context.Background(), app, args)
	require.NoError(t, err)
	require.Equal(t, 0, retCode)
	compareResults(t, expected, buf)
}

func TestRelativeRepoAndModDirs(t *testing.T) {
	t.Parallel()
	configs := loadTestConfigs(t)
	patchName := "change-in-top-level-package.patch"
	expected := configs[patchName]

	absWorktreePath := setupWorktree(t, "changed-test-relative-paths")
	cwd, err := os.Getwd()
	require.NoError(t, err)
	worktreePath, err := filepath.Rel(cwd, absWorktreePath)
	if err != nil && isWindows() {
		t.Skip("skipping relative path test on windows")
	}
	require.NoError(t, err)
	// the relative path might be a symlink (seen on GitHub action MacOS runner)
	worktreePath, err = filepath.EvalSymlinks(worktreePath)
	require.NoError(t, err)
	modDir := filepath.Join(worktreePath, modPath)

	prePatchHead, postPatchHead := commitPatches(t, worktreePath, patchName)

	var buf bytes.Buffer
	args := append( //nolint:gocritic
		progArgs,
		"--repo-dir",
		worktreePath,
		"--mod-dir",
		modDir,
		"--from-ref",
		prePatchHead,
		"--to-ref",
		postPatchHead,
	)
	app := buildTestApp(&buf)
	retCode, err := runApp(context.Background(), app, args)
	require.NoError(t, err)
	require.Equal(t, 0, retCode)
	compareResults(t, expected, buf)
}

func compareResults(t *testing.T, expected []string, buf bytes.Buffer) {
	t.Helper()
	var gotLines []string
	got := buf.String()
	if got == "" {
		gotLines = []string{}
	} else {
		gotLines = strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	}

	require.ElementsMatch(t, expected, gotLines)
}

func commitPatches(t *testing.T, repoDir string, patchNames ...string) (string, string) {
	t.Helper()
	patchPaths := make([]string, 0, len(patchNames))
	patchesPath := getPatchesPath(t)
	for _, patchName := range patchNames {
		patchPaths = append(patchPaths, filepath.Join(patchesPath, patchName))
	}

	prePatchHead := getHeadCommit(t, repoDir)

	for _, patch := range patchPaths {
		mustRunGitCmd(t, "-C", repoDir, "apply", "--index", patch)
		mustRunGitCmd(
			t,
			// avoid relying on local config to set user info
			"-c",
			"user.name=releaser-test",
			"-c",
			"user.email=releaser-test@example.com",
			"-C",
			repoDir,
			"commit",
			// avoid running any hooks that might be configured locally (e.g.
			// from `pre-commit`)
			"--no-verify",
			"--message",
			patch,
		)
	}

	postPatchHead := getHeadCommit(t, repoDir)
	return prePatchHead, postPatchHead
}

func TestErrorsWhenFailsToReadPackages(t *testing.T) {
	if isWindows() {
		t.Skip("Not running on windows: uses chmod with unix permissions")
	}
	t.Parallel()
	// create directory we don't have permission to search
	modDir := filepath.Join(t.TempDir(), "moddir")
	require.NoError(t, os.Mkdir(modDir, 0o666))

	args := append( //nolint:gocritic
		progArgs,
		"--mod-dir",
		modDir,
		"--from-ref",
		"",
		"--to-ref",
		"",
	)
	app := buildTestApp(io.Discard)
	retCode, err := runApp(context.Background(), app, args)

	require.Equal(t, 1, retCode)
	require.ErrorContains(t, err, "failed listing local packages: ")
}

func TestErrorsWhenFailstoListingChangedFiles(t *testing.T) {
	t.Parallel()
	// directory isn't a Git repo
	repoDir := t.TempDir()

	args := append( //nolint:gocritic
		progArgs,
		"--repo-dir",
		repoDir,
		"--from-ref",
		"",
		"--to-ref",
		"",
	)
	app := buildTestApp(io.Discard)
	retCode, err := runApp(context.Background(), app, args)

	require.Equal(t, 1, retCode)
	require.ErrorContains(t, err, "listing changed files: running command: `git")
}

func TestErrorsWhenFailsToReadingGoMod(t *testing.T) {
	t.Parallel()
	worktreeName := "remove-go-mod"
	// the error uses the path given to us from `git diff`, and that uses `/`
	// as the path separator, even on windows
	reportedModPath := strings.Join(strings.Split(modPath, string(filepath.Separator)), "/")

	err := runWithPatches(
		t,
		setupWorktree(t, worktreeName),
		[]string{"remove-go-mod.patch"},
		io.Discard,
	)

	require.ErrorContains(t, err, "reading "+reportedModPath+"/go.mod")
}

func TestErrorsWhenFailingToParseGoMod(t *testing.T) {
	t.Parallel()
	worktreePath := setupWorktree(t, "break-go-mod")
	// the error uses the path given to us from `git diff`, and that uses `/`
	// as the path separator, even on windows
	reportedModPath := strings.Join(strings.Split(modPath, string(filepath.Separator)), "/")

	// break `go.mod`...
	_, headSha := commitPatches(t, worktreePath, "break-go-mod.patch")

	//  ...and then fix it again so we can process packages at HEAD
	err := runWithPatches(t, worktreePath, []string{"fix-go-mod.patch"}, io.Discard)

	require.ErrorContains(
		t,
		err,
		"parsing mod file "+reportedModPath+"/go.mod"+" at "+headSha,
	)
}

func TestErrorsWhenFailsToQueryPackage(t *testing.T) {
	t.Parallel()

	err := runWithPatches(
		t,
		setupWorktree(t, "error-in-package"),
		[]string{"syntax-error-in-package.patch"},
		io.Discard,
	)

	require.ErrorContains(t, err, "failed querying package "+testModuleName)
}

func mustRunGitCmd(t *testing.T, args ...string) string {
	t.Helper()

	stdout, err := git.RunGitCmd(context.Background(), args...)
	require.NoError(t, err)
	return stdout
}

func getHeadCommit(t *testing.T, repoPath string) string {
	t.Helper()

	out := mustRunGitCmd(t, "-C", repoPath, "log", "--format=%H", "--max-count", "1")
	return strings.TrimSuffix(out, "\n")
}

func buildTestApp(out io.Writer) *cli.App {
	app := buildApp(out)

	// avoiding race condition: https://github.com/urfave/cli/issues/1670
	app.HideHelp = true
	app.HideVersion = true

	return app
}
