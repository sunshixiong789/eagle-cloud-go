package architecture_test

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestLayerDependencies(t *testing.T) {
	root := repositoryRoot()
	for _, module := range discoverModules(t, root) {
		basePath := "./app/" + module.service + "/internal/" + module.module
		baseImport := "github.com/eagle-go/eagle/app/" + module.service + "/internal/" + module.module

		assertImports(t, root, basePath+"/domain", nil, func(imp string) bool {
			return !isStandardImport(imp)
		})

		applicationDir := filepath.Join(root, "app", module.service, "internal", module.module, "application")
		if isDirectory(applicationDir) {
			assertImports(t, root, basePath+"/application", []string{baseImport + "/domain"}, func(imp string) bool {
				return !isStandardImport(imp)
			})
		}

		serviceDir := filepath.Join(root, "app", module.service, "internal", module.module, "service")
		if isDirectory(serviceDir) {
			assertImports(t, root, basePath+"/service", nil, func(imp string) bool {
				return strings.HasPrefix(imp, baseImport+"/infrastructure") ||
					strings.Contains(imp, "/internal/platform/database")
			})
		}

		infrastructureDir := filepath.Join(root, "app", module.service, "internal", module.module, "infrastructure")
		if isDirectory(infrastructureDir) {
			assertImports(t, root, basePath+"/infrastructure/...", nil, func(imp string) bool {
				return strings.HasPrefix(imp, baseImport+"/service")
			})
		}
	}
}

func TestServiceCompositionBoundaries(t *testing.T) {
	root := repositoryRoot()
	services := discoverServices(t, root)
	for _, service := range services {
		for _, pkg := range listPackages(t, root, "./app/"+service+"/...") {
			for _, imp := range pkg.Imports {
				const appPrefix = "github.com/eagle-go/eagle/app/"
				if !strings.HasPrefix(imp, appPrefix) {
					continue
				}
				owner := strings.Split(strings.TrimPrefix(imp, appPrefix), "/")[0]
				if owner != service {
					t.Errorf("service %s imports service %s implementation package %s", service, owner, imp)
				}
			}
		}
	}
}

type listedPackage struct {
	ImportPath string
	Imports    []string
}

func listPackages(t *testing.T, root, pattern string) []listedPackage {
	t.Helper()
	cmd := exec.Command("go", "list", "-json", pattern)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list %s: %v", pattern, err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(out)))
	var packages []listedPackage
	for {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("decode go list %s: %v", pattern, err)
		}
		packages = append(packages, pkg)
	}
	return packages
}

type modulePath struct {
	service string
	module  string
}

func repositoryRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func discoverServices(t *testing.T, root string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(root, "app", "*", "go.mod"))
	if err != nil {
		t.Fatalf("discover services: %v", err)
	}
	services := make([]string, 0, len(paths))
	for _, path := range paths {
		services = append(services, filepath.Base(filepath.Dir(path)))
	}
	slices.Sort(services)
	return services
}

func discoverModules(t *testing.T, root string) []modulePath {
	t.Helper()
	var modules []modulePath
	for _, service := range discoverServices(t, root) {
		paths, err := filepath.Glob(filepath.Join(root, "app", service, "internal", "*", "domain"))
		if err != nil {
			t.Fatalf("discover modules of %s: %v", service, err)
		}
		for _, path := range paths {
			modules = append(modules, modulePath{service: service, module: filepath.Base(filepath.Dir(path))})
		}
	}
	return modules
}

func assertImports(t *testing.T, root, pkg string, allowed []string, forbidden func(string) bool) {
	t.Helper()
	t.Run(pkg, func(t *testing.T) {
		cmd := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}`, pkg)
		cmd.Dir = root
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list: %v", err)
		}
		for _, imp := range strings.Fields(string(out)) {
			if slices.Contains(allowed, imp) {
				continue
			}
			if forbidden(imp) {
				t.Errorf("%s must not import %s", pkg, imp)
			}
		}
	})
}

func isStandardImport(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func TestInfrastructureBoundaries(t *testing.T) {
	root := repositoryRoot()

	for _, pkg := range listPackages(t, root, "./pkg/...") {
		for _, imp := range pkg.Imports {
			if strings.HasPrefix(imp, "github.com/eagle-go/eagle/app/") {
				t.Errorf("shared package %s must not depend on application package %s", pkg.ImportPath, imp)
			}
		}
	}

	for _, service := range discoverServices(t, root) {
		for _, pkg := range listPackages(t, root, "./app/"+service+"/...") {
			if strings.Contains(pkg.ImportPath, "/infrastructure") ||
				strings.Contains(pkg.ImportPath, "/internal/platform/database") ||
				strings.Contains(pkg.ImportPath, "/tests/") {
				continue
			}
			for _, imp := range pkg.Imports {
				if strings.Contains(imp, "/internal/platform/database/ent") {
					t.Errorf("%s imports persistence package %s outside infrastructure", pkg.ImportPath, imp)
				}
			}
		}
	}
}

// 这些库是 AI 生成代码时最常顺手引进、但本仓库已明确拒绝的。
// 标准库 slices/maps/cmp、encoding/json、errors、log/slog 已经覆盖对应需求。
var bannedModulePrefixes = []string{
	"github.com/samber/lo",
	"github.com/duke-git/lancet",
	"github.com/jinzhu/copier",
	"github.com/mitchellh/mapstructure",
	"github.com/spf13/cast",
	"github.com/pkg/errors",
	"github.com/go-kratos/kratos/v2",
	"github.com/sirupsen/logrus",
	"go.uber.org/zap",
	"gorm.io/gorm",
	"github.com/go-redis/redis/v8",
	"github.com/go-redis/redis/v7",
	"github.com/redis/go-redis",
}

func TestWireOnlyInCompositionRoot(t *testing.T) {
	root := repositoryRoot()
	for _, service := range discoverServices(t, root) {
		for _, pkg := range listPackages(t, root, "./app/"+service+"/...") {
			usesWire := slices.Contains(pkg.Imports, "github.com/google/wire")
			if !usesWire {
				continue
			}
			if !strings.HasSuffix(pkg.ImportPath, "/cmd/"+service) {
				t.Errorf("%s imports github.com/google/wire; Wire stays in app/%s/cmd/%s", pkg.ImportPath, service, service)
			}
		}
	}
}

func TestBannedDependencies(t *testing.T) {
	root := repositoryRoot()

	cmd := exec.Command("go", "list", "-m", "-f", "{{if not .Indirect}}{{.Path}}{{end}}", "all")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -m all: %v", err)
	}

	for _, mod := range strings.Fields(string(out)) {
		for _, prefix := range bannedModulePrefixes {
			if mod == prefix || strings.HasPrefix(mod, prefix+"/") {
				t.Errorf("forbidden module %s (matched %s); use the standard library or existing pkg/ instead, see .agents/rules/style.md", mod, prefix)
			}
		}
	}
}
