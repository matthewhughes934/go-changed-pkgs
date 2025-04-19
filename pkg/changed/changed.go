package changed

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"gitlab.com/matthewhughes/slogctx"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/tools/go/packages"

	"github.com/matthewhughes934/go-changed-pkgs/internal/git"
)

// get packages that are changed between `fromRef` and `toRef`, where 'changed'
// means:
//
//   - The package contains a file that was changed between the two SHAs
//   - The package imports a package from a 3rd party module that was changed between the to SHAs
//   - The package imports a local package for which either of the above holds
//
// For a 3rd party module to be recorded as changed it must satisfy one of:
//
//   - Be included though a plain 'require' and have it's version changed, or
//   - Be on the right side of a 'replace' that was added, updated, or removed between the two references
func GetChangedPackages(
	ctx context.Context,
	logger *slog.Logger,
	repoDir string,
	modDir string,
	fromRef string,
	toRef string,
) ([]string, error) {
	ctx = slogctx.WithLogger(ctx, logger)
	pkgs, err := loadLocalPackages(ctx, modDir)
	if err != nil {
		return nil, err
	}

	// some bits require an absolute path, some don't. For simplicity just
	// always use an absolute path
	repoDir, err = filepath.Abs(repoDir)
	if err != nil { //go-cov:skip // this is a bit of a hassle to test, and we don't really ever expect a failure
		return nil, fmt.Errorf("failed building absolute path for %s: %w", repoDir, err)
	}

	changedFiles, err := getChangedFiles(ctx, repoDir, fromRef, toRef)
	if err != nil {
		return nil, err
	}
	slogctx.FromContext(ctx).Info("changed files", "files", changedFiles)

	changedPackages, changedMods, err := collectChanges(
		ctx,
		changedFiles,
		pkgs,
		repoDir,
		fromRef,
		toRef,
	)
	if err != nil {
		return nil, err
	}

	// relies on package's dependencies being before the package itself in the list
	// this is guaranteed by `loadLocalPackages`
	for _, pkg := range pkgs {
		if _, ok := changedPackages[pkg.PkgPath]; ok {
			continue
		}
		if isChangedFromImports(ctx, pkg.PkgPath, pkg.Imports, changedMods, changedPackages) {
			changedPackages[pkg.PkgPath] = struct{}{}
		}
	}

	return slices.AppendSeq(
		make([]string, 0, len(changedPackages)),
		maps.Keys(changedPackages),
	), nil
}

func loadLocalPackages(ctx context.Context, modDir string) ([]*packages.Package, error) {
	loadCfg := packages.Config{
		Context: ctx,
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedEmbedFiles |
			// this runs `go list` with `-deps` which means
			// "... a package is listed only after all its dependencies" (see the `go list` docs)
			packages.NeedImports |
			packages.NeedDeps |
			// include the module: so we can map imports of 3rd party packages
			// to a module

			packages.NeedModule,
		Dir: modDir,
	}
	pkgs, err := packages.Load(&loadCfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("failed listing local packages: %w", err)
	}

	// early check for errors in packages, e.g. we can't load one because of a
	// syntax error in the source
	for _, pkg := range pkgs {
		if len(pkg.Errors) != 0 {
			return nil, fmt.Errorf("failed querying package %s: %v", pkg.PkgPath, pkg.Errors)
		}
	}

	return pkgs, nil
}

func getChangedFiles(
	ctx context.Context,
	repoDir string,
	fromRef string,
	toRef string,
) ([]string, error) {
	out, err := git.RunGitCmd(ctx, "-C", repoDir, "diff", "--name-only", "-z", fromRef, toRef)
	if err != nil {
		return nil, fmt.Errorf("listing changed files: %w", err)
	}

	// there's always a trailing '\x00' so trim that element
	return strings.Split(strings.TrimSuffix(out, "\x00"), "\x00"), nil
}

func collectChanges(
	ctx context.Context,
	changedFiles []string,
	pkgs []*packages.Package,
	repoDir string,
	fromRef string,
	toRef string,
) (map[string]struct{}, map[string]struct{}, error) {
	changedPackages := map[string]struct{}{}
	changedMods := map[string]struct{}{}
	var err error

	for _, path := range changedFiles {
		if filepath.Base(path) == "go.mod" {
			changedMods, err = getChangedMods(ctx, path, repoDir, fromRef, toRef)
			if err != nil {
				return nil, nil, err
			}
			slogctx.FromContext(ctx).Info("changed 3rd party modules", "modules", changedMods)
		}

		for _, pkg := range pkgs {
			if fileInPkg(pkg, repoDir, path) {
				slogctx.FromContext(ctx).Debug(
					"package detected changed because of file",
					"package",
					pkg.PkgPath,
					"file",
					path,
				)
				changedPackages[pkg.PkgPath] = struct{}{}
				// a file shouldn't belong to more than one package
				break
			}
		}
	}

	return changedPackages, changedMods, nil
}

func getChangedMods(
	ctx context.Context,
	modPath string,
	repoDir string,
	fromRef string,
	toRef string,
) (map[string]struct{}, error) {
	curModFile, oldModFile, err := readModFiles(ctx, repoDir, modPath, fromRef, toRef)
	if err != nil {
		return nil, err
	}
	changedMods := changedModsFromReplace(curModFile, oldModFile)

	// we're not interested in modules that existed in the old go.mod
	// but not the current one, since no packages should currently
	// depend on them
	oldModMap := map[string]*modfile.Require{}
	for _, req := range oldModFile.Require {
		oldModMap[req.Mod.Path] = req
	}
	for _, req := range curModFile.Require {
		if old, ok := oldModMap[req.Mod.Path]; ok {
			if req.Mod.Version != old.Mod.Version {
				changedMods[req.Mod.Path] = struct{}{}
			}
		}
	}

	return changedMods, nil
}

func changedModsFromReplace(
	curModFile *modfile.File,
	oldModFile *modfile.File,
) map[string]struct{} {
	changedMods := map[string]struct{}{}
	oldReplaceMap := map[string]module.Version{}
	for _, replace := range oldModFile.Replace {
		oldReplaceMap[replace.Old.Path] = replace.New
	}

	for _, replace := range curModFile.Replace {
		path := replace.Old.Path
		oldVersion, ok := oldReplaceMap[path]
		if !ok {
			// added a replace, mark the module as updated
			changedMods[replace.Old.Path] = struct{}{}
			continue
		}

		// updated right side of a replace directive
		if oldVersion != replace.New {
			changedMods[replace.Old.Path] = struct{}{}
		}
		delete(oldReplaceMap, path)
	}

	// replaces that previously existed, but no longer do: we must've replaced
	// them
	for path := range oldReplaceMap {
		changedMods[path] = struct{}{}
	}

	return changedMods
}

func readModFiles(
	ctx context.Context,
	repoDir string,
	modPath string,
	fromRef string,
	toRef string,
) (*modfile.File, *modfile.File, error) {
	oldModFile, err := readModFileAtRef(ctx, repoDir, modPath, fromRef)
	if err != nil {
		return nil, nil, err
	}
	newModFile, err := readModFileAtRef(ctx, repoDir, modPath, toRef)
	if err != nil {
		return nil, nil, err
	}

	return newModFile, oldModFile, nil
}

func readModFileAtRef(
	ctx context.Context,
	repoDir string,
	modPath string,
	ref string,
) (*modfile.File, error) {
	modData, err := git.RunGitCmd(
		ctx,
		"-C",
		repoDir,
		"show",
		fmt.Sprintf("%s:%s", ref, modPath),
	)
	if err != nil {
		return nil, fmt.Errorf("reading %s at %s: %w", modPath, ref, err)
	}
	modFile, err := modfile.Parse(modPath, []byte(modData), nil)
	if err != nil {
		return nil, fmt.Errorf("parsing mod file %s at %s: %w", modPath, ref, err)
	}
	return modFile, nil
}

func fileInPkg(pkg *packages.Package, repoDir string, path string) bool {
	// packages.Package uses absolute paths for files
	absPath := filepath.Join(repoDir, path)

	return slices.Contains(pkg.GoFiles, absPath) ||
		slices.Contains(pkg.OtherFiles, absPath) ||
		slices.Contains(pkg.EmbedFiles, absPath)
}

func isChangedFromImports(
	ctx context.Context,
	pkgPath string,
	imports map[string]*packages.Package,
	changedMods map[string]struct{},
	changedPackages map[string]struct{},
) bool {
	for importPath, importPkg := range imports {
		if _, ok := changedPackages[importPath]; ok {
			slogctx.FromContext(ctx).Debug(
				"package detected changed because of dependent package",
				"package",
				pkgPath,
				"dependency",
				importPath,
			)
			return true
		}
		mod := importPkg.Module
		if mod != nil {
			if _, ok := changedMods[mod.Path]; ok {
				slogctx.FromContext(ctx).Debug(
					"package detected changed because of dependent 3rd party module",
					"package",
					pkgPath,
					"module",
					mod.Path,
				)
				return true
			}
		}
	}
	return false
}
