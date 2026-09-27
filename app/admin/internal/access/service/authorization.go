package service

import (
	"context"

	accessv1 "github.com/eagle-go/eagle/api/eagle/access/v1"
	"github.com/eagle-go/eagle/app/admin/internal/access/domain"
)

type AuthorizationService struct {
	accessv1.UnimplementedAuthorizationServiceServer
	checker domain.AuthorizationChecker
}

func NewAuthorizationService(checker domain.AuthorizationChecker) *AuthorizationService {
	return &AuthorizationService{checker: checker}
}

func (s *AuthorizationService) CheckPermission(ctx context.Context, req *accessv1.CheckPermissionRequest) (*accessv1.CheckPermissionResponse, error) {
	code, err := domain.NewPermissionCode(req.GetPermission())
	if err != nil {
		return nil, err
	}
	allowed, err := s.checker.Allow(ctx, req.GetRoles(), code)
	if err != nil {
		return nil, err
	}
	version, err := s.checker.Version(ctx)
	if err != nil {
		return nil, err
	}
	return &accessv1.CheckPermissionResponse{Allowed: allowed, PolicyVersion: version}, nil
}

func (s *AuthorizationService) GetPolicySnapshot(ctx context.Context, _ *accessv1.GetPolicySnapshotRequest) (*accessv1.GetPolicySnapshotResponse, error) {
	rules, version, err := s.checker.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*accessv1.PolicyRule, 0, len(rules))
	for _, rule := range rules {
		out = append(out, &accessv1.PolicyRule{Ptype: rule.PType, Values: rule.Values})
	}
	return &accessv1.GetPolicySnapshotResponse{Rules: out, PolicyVersion: version}, nil
}
