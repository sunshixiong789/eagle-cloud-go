package service

import (
	"context"

	v1 "github.com/eagle-go/eagle/api/eagle/dictionary/v1"
	"github.com/eagle-go/eagle/app/admin/internal/dictionary/domain"
)

// DictService 实现 v1.DictService。
type DictService struct {
	v1.UnimplementedDictServiceServer

	repo domain.DictRepo
}

// NewDictService 构造字典服务。
func NewDictService(repo domain.DictRepo) *DictService {
	return &DictService{repo: repo}
}

func toProtoDictType(t *domain.DictType) *v1.DictType {
	if t == nil {
		return nil
	}
	return &v1.DictType{
		Id:        t.ID,
		Name:      t.Name,
		Type:      t.Type,
		Status:    fromStatus(t.Status),
		Remark:    t.Remark,
		CreatedAt: ts(t.CreatedAt),
		UpdatedAt: ts(t.UpdatedAt),
	}
}

func toProtoDictData(d *domain.DictData) *v1.DictData {
	if d == nil {
		return nil
	}
	return &v1.DictData{
		Id:        d.ID,
		DictType:  d.DictType,
		Label:     d.Label,
		Value:     d.Value,
		Sort:      d.Sort,
		CssClass:  d.CSSClass,
		IsDefault: d.IsDefault,
		Status:    fromStatus(d.Status),
		Remark:    d.Remark,
		CreatedAt: ts(d.CreatedAt),
		UpdatedAt: ts(d.UpdatedAt),
	}
}

// ── 字典类型 ──────────────────────────────────────────────

// CreateDictType 新建字典类型。
func (s *DictService) CreateDictType(ctx context.Context, req *v1.CreateDictTypeRequest) (*v1.CreateDictTypeResponse, error) {
	t, err := s.repo.CreateType(ctx, &domain.DictType{
		Name:   req.GetName(),
		Type:   req.GetType(),
		Status: toStatus(req.GetStatus()),
		Remark: req.GetRemark(),
	})
	if err != nil {
		return nil, err
	}
	return &v1.CreateDictTypeResponse{DictType: toProtoDictType(t)}, nil
}

// ListDictTypes 分页查询字典类型。
func (s *DictService) ListDictTypes(ctx context.Context, req *v1.ListDictTypesRequest) (*v1.ListDictTypesResponse, error) {
	offset, limit := paginate(req.GetPage(), req.GetPageSize())

	types, total, err := s.repo.ListTypes(ctx, domain.ListDictTypesQuery{
		Keyword:  req.GetKeyword(),
		Status:   toStatusPtr(req.Status),
		Offset:   offset,
		PageSize: limit,
	})
	if err != nil {
		return nil, err
	}

	out := make([]*v1.DictType, 0, len(types))
	for _, t := range types {
		out = append(out, toProtoDictType(t))
	}
	return &v1.ListDictTypesResponse{DictTypes: out, Total: total}, nil
}

// UpdateDictType 更新字典类型。
func (s *DictService) UpdateDictType(ctx context.Context, req *v1.UpdateDictTypeRequest) (*v1.UpdateDictTypeResponse, error) {
	t, err := s.repo.UpdateType(ctx, &domain.DictType{
		ID:     req.GetId(),
		Name:   req.GetName(),
		Status: toStatus(req.GetStatus()),
		Remark: req.GetRemark(),
	})
	if err != nil {
		return nil, err
	}
	return &v1.UpdateDictTypeResponse{DictType: toProtoDictType(t)}, nil
}

// DeleteDictType 删除字典类型，其下字典项由外键级联删除。
func (s *DictService) DeleteDictType(ctx context.Context, req *v1.DeleteDictTypeRequest) (*v1.DeleteDictTypeResponse, error) {
	if err := s.repo.DeleteType(ctx, req.GetId()); err != nil {
		return nil, err
	}
	return &v1.DeleteDictTypeResponse{}, nil
}

// ── 字典项 ────────────────────────────────────────────────

// CreateDictData 新建字典项。
func (s *DictService) CreateDictData(ctx context.Context, req *v1.CreateDictDataRequest) (*v1.CreateDictDataResponse, error) {
	d, err := s.repo.CreateData(ctx, &domain.DictData{
		DictType:  req.GetDictType(),
		Label:     req.GetLabel(),
		Value:     req.GetValue(),
		Sort:      req.GetSort(),
		CSSClass:  req.GetCssClass(),
		IsDefault: req.GetIsDefault(),
		Status:    toStatus(req.GetStatus()),
		Remark:    req.GetRemark(),
	})
	if err != nil {
		return nil, err
	}
	return &v1.CreateDictDataResponse{DictData: toProtoDictData(d)}, nil
}

// ListDictData 分页查询字典项。
func (s *DictService) ListDictData(ctx context.Context, req *v1.ListDictDataRequest) (*v1.ListDictDataResponse, error) {
	offset, limit := paginate(req.GetPage(), req.GetPageSize())

	data, total, err := s.repo.ListData(ctx, domain.ListDictDataQuery{
		DictType: req.DictType,
		Keyword:  req.GetKeyword(),
		Status:   toStatusPtr(req.Status),
		Offset:   offset,
		PageSize: limit,
	})
	if err != nil {
		return nil, err
	}

	out := make([]*v1.DictData, 0, len(data))
	for _, d := range data {
		out = append(out, toProtoDictData(d))
	}
	return &v1.ListDictDataResponse{DictData: out, Total: total}, nil
}

// UpdateDictData 更新字典项。
func (s *DictService) UpdateDictData(ctx context.Context, req *v1.UpdateDictDataRequest) (*v1.UpdateDictDataResponse, error) {
	d, err := s.repo.UpdateData(ctx, domain.UpdateDictData{
		ID:        req.GetId(),
		Label:     req.GetLabel(),
		Value:     req.GetValue(),
		Sort:      req.GetSort(),
		CSSClass:  req.GetCssClass(),
		IsDefault: req.GetIsDefault(),
		Status:    toStatus(req.GetStatus()),
		Remark:    req.GetRemark(),
	})
	if err != nil {
		return nil, err
	}
	return &v1.UpdateDictDataResponse{DictData: toProtoDictData(d)}, nil
}

// DeleteDictData 删除字典项。
func (s *DictService) DeleteDictData(ctx context.Context, req *v1.DeleteDictDataRequest) (*v1.DeleteDictDataResponse, error) {
	if err := s.repo.DeleteData(ctx, req.GetId()); err != nil {
		return nil, err
	}
	return &v1.DeleteDictDataResponse{}, nil
}

// GetDictDataByType 按类型取字典项，供前端渲染下拉框。
func (s *DictService) GetDictDataByType(ctx context.Context, req *v1.GetDictDataByTypeRequest) (*v1.GetDictDataByTypeResponse, error) {
	data, err := s.repo.ListDataByType(ctx, req.GetDictType())
	if err != nil {
		return nil, err
	}

	out := make([]*v1.DictData, 0, len(data))
	for _, d := range data {
		out = append(out, toProtoDictData(d))
	}
	return &v1.GetDictDataByTypeResponse{DictData: out}, nil
}
