package service

import (
	"errors"
	"github-cmdb/internal/model"
	"github-cmdb/internal/repository"
)

type CITypeSvc struct {
	repo *repository.CITypeRepo
}

func NewCITypeSvc(repo *repository.CITypeRepo) *CITypeSvc {
	return &CITypeSvc{repo: repo}
}

func (s *CITypeSvc) ListTree() ([]model.CIType, error) {
	all, err := s.repo.List()
	if err != nil { return nil, err }
	return buildTree(all, nil), nil
}

func buildTree(all []model.CIType, parentID *uint64) []model.CIType {
	var result []model.CIType
	for _, t := range all {
		if (parentID == nil && t.ParentID == nil) || (parentID != nil && t.ParentID != nil && *t.ParentID == *parentID) {
			children := buildTree(all, &t.ID)
			if len(children) > 0 { t.Children = children }
			result = append(result, t)
		}
	}
	return result
}

func (s *CITypeSvc) Get(id uint64) (*model.CIType, error) { return s.repo.GetByIDWithAttributes(id) }
func (s *CITypeSvc) Create(t *model.CIType) error         { return s.repo.Create(t) }
func (s *CITypeSvc) Update(t *model.CIType) error         { return s.repo.Update(t) }

func (s *CITypeSvc) Delete(id uint64) error {
	has, err := s.repo.HasInstances(id)
	if err != nil { return err }
	if has { return errors.New("cannot delete type: instances exist") }
	return s.repo.Delete(id)
}

func (s *CITypeSvc) ListAttributes(typeID uint64) ([]model.CIAttribute, error) { return s.repo.ListAttributes(typeID) }
func (s *CITypeSvc) CreateAttribute(attr *model.CIAttribute) error             { return s.repo.CreateAttribute(attr) }
func (s *CITypeSvc) UpdateAttribute(attr *model.CIAttribute) error             { return s.repo.UpdateAttribute(attr) }
func (s *CITypeSvc) DeleteAttribute(id uint64) error                            { return s.repo.DeleteAttribute(id) }