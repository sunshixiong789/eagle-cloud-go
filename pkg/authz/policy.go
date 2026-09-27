package authz

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	annotationsv1 "github.com/eagle-go/eagle/api/eagle/annotations/v1"
)

// Policy 是某个 RPC 方法的访问策略，来自 proto 上的
// eagle.annotations.v1.perm / public 扩展。
type Policy struct {
	// Perm 是调用所需的权限码，空表示只校验登录态。
	Perm string
	// Public 表示免登录。
	Public bool
	// Known 表示成功解析到了方法描述符。
	//
	// 解析不到时（比如 gRPC 反射、健康检查这类非本项目定义的方法）
	// 中间件直接拒绝——失败方向选择关闭而非放行。
	Known bool
	// Access 是 RPC 显式声明的访问级别。
	Access annotationsv1.AccessLevel
}

// 描述符查找要走全局注册表并做接口断言，开销不小，
// 而 operation 集合是有限且固定的，所以查一次就缓存住。
var policyCache sync.Map // operation(string) -> Policy

// PolicyFor 解析 Kratos 的 operation（形如 "/eagle.access.v1.PermissionService/CreatePermission"）
// 并返回该方法的访问策略。
func PolicyFor(operation string) Policy {
	if v, ok := policyCache.Load(operation); ok {
		p, _ := v.(Policy)
		return p
	}
	p := lookupPolicy(operation)
	policyCache.Store(operation, p)
	return p
}

func lookupPolicy(operation string) Policy {
	serviceName, methodName, ok := splitOperation(operation)
	if !ok {
		return Policy{}
	}

	desc, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(serviceName))
	if err != nil {
		return Policy{}
	}
	sd, ok := desc.(protoreflect.ServiceDescriptor)
	if !ok {
		return Policy{}
	}
	md := sd.Methods().ByName(protoreflect.Name(methodName))
	if md == nil {
		return Policy{}
	}
	opts, ok := md.Options().(*descriptorpb.MethodOptions)
	if !ok || opts == nil {
		return Policy{Known: true}
	}

	p := Policy{Known: true}
	if v, ok := proto.GetExtension(opts, annotationsv1.E_Perm).(string); ok {
		p.Perm = v
	}
	if v, ok := proto.GetExtension(opts, annotationsv1.E_Public).(bool); ok {
		p.Public = v
	}
	if v, ok := proto.GetExtension(opts, annotationsv1.E_Access).(annotationsv1.AccessLevel); ok {
		p.Access = v
	}
	// 兼容旧契约；新契约必须使用 access。
	if p.Access == annotationsv1.AccessLevel_ACCESS_LEVEL_PUBLIC {
		p.Public = true
	}
	return p
}

var concretePermissionPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]*:[a-zA-Z][a-zA-Z0-9]*:[a-zA-Z][a-zA-Z0-9]*$`)

// ValidateRegisteredPolicies 扫描所有 eagle RPC 的访问声明，并可选地校验
// 所需权限码是否存在于数据库权限目录。服务启动时执行，避免漏注解的接口
// 带着不安全的默认语义运行。
func ValidateRegisteredPolicies(catalogCodes []string) error {
	catalog := make(map[string]struct{}, len(catalogCodes))
	for _, code := range catalogCodes {
		catalog[code] = struct{}{}
	}

	var violations []string
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if !strings.HasPrefix(string(fd.Package()), "eagle.") {
			return true
		}
		services := fd.Services()
		for i := 0; i < services.Len(); i++ {
			svc := services.Get(i)
			methods := svc.Methods()
			for j := 0; j < methods.Len(); j++ {
				method := methods.Get(j)
				op := "/" + string(svc.FullName()) + "/" + string(method.Name())
				policy := lookupPolicy(op)
				switch policy.Access {
				case annotationsv1.AccessLevel_ACCESS_LEVEL_PUBLIC,
					annotationsv1.AccessLevel_ACCESS_LEVEL_AUTHENTICATED,
					annotationsv1.AccessLevel_ACCESS_LEVEL_INTERNAL:
					if policy.Perm != "" {
						violations = append(violations, op+": 非权限访问级别不能同时声明 perm")
					}
				case annotationsv1.AccessLevel_ACCESS_LEVEL_PERMISSION_REQUIRED:
					if !concretePermissionPattern.MatchString(policy.Perm) {
						violations = append(violations, fmt.Sprintf("%s: 权限码 %q 不是严格三段格式", op, policy.Perm))
					} else if len(catalog) > 0 {
						if _, ok := catalog[policy.Perm]; !ok {
							violations = append(violations, fmt.Sprintf("%s: 权限码 %q 不在权限目录中", op, policy.Perm))
						}
					}
				default:
					violations = append(violations, op+": 未显式声明 access")
				}
			}
		}
		return true
	})
	if len(violations) == 0 {
		return nil
	}
	sort.Strings(violations)
	return fmt.Errorf("authz: RPC 访问契约无效:\n  - %s", strings.Join(violations, "\n  - "))
}

// splitOperation 把 "/pkg.Service/Method" 拆成服务全名和方法名。
func splitOperation(operation string) (service, method string, ok bool) {
	s := strings.TrimPrefix(operation, "/")
	idx := strings.LastIndex(s, "/")
	if idx <= 0 || idx == len(s)-1 {
		return "", "", false
	}
	return s[:idx], s[idx+1:], true
}
