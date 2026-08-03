package service

import (
	"errors"
	"time"

	"github-cmdb/internal/model"
	"github-cmdb/internal/repository"
)

type ChangeSvc struct {
	repo     *repository.ChangeRepo
	ciRepo   *repository.CIInstanceRepo
	snapRepo *repository.ConfigSnapshotRepo
}

func NewChangeSvc(repo *repository.ChangeRepo, ciRepo *repository.CIInstanceRepo, snapRepo *repository.ConfigSnapshotRepo) *ChangeSvc {
	return &ChangeSvc{repo: repo, ciRepo: ciRepo, snapRepo: snapRepo}
}

func (s *ChangeSvc) List(filter repository.ChangeFilter) ([]model.ChangeTicket, int64, error) {
	return s.repo.List(filter)
}

func (s *ChangeSvc) Get(id uint64) (*model.ChangeTicket, error) {
	return s.repo.GetByID(id)
}

func (s *ChangeSvc) Create(ticket *model.ChangeTicket) error {
	ticket.Status = "draft"
	ci, err := s.ciRepo.GetByID(ticket.CITargetID)
	if err != nil { return errors.New("target CI not found") }
	ticket.CITargetName = ci.Name
	return s.repo.Create(ticket)
}

func (s *ChangeSvc) Update(ticket *model.ChangeTicket) error {
	return s.repo.Update(ticket)
}

func (s *ChangeSvc) Delete(id uint64) error {
	return s.repo.Delete(id)
}

func (s *ChangeSvc) Submit(id uint64) error {
	ticket, err := s.repo.GetByID(id)
	if err != nil { return err }
	if ticket.Status != "draft" { return errors.New("only draft tickets can be submitted") }
	ticket.Status = "pending_approval"
	return s.repo.Update(ticket)
}

func (s *ChangeSvc) Approve(id uint64, approvedBy string) error {
	ticket, err := s.repo.GetByID(id)
	if err != nil { return err }
	if ticket.Status != "pending_approval" { return errors.New("only pending tickets can be approved") }
	now := time.Now()
	ticket.Status = "approved"
	ticket.ApprovedBy = &approvedBy
	ticket.ApprovedAt = &now
	// optional: snapshot before
	if snap, err := s.snapRepo.GetLatestByCIID(ticket.CITargetID); err == nil {
		ticket.BeforeSnapshotID = &snap.ID
	}
	return s.repo.Update(ticket)
}

func (s *ChangeSvc) Reject(id uint64, rejectedBy string) error {
	ticket, err := s.repo.GetByID(id)
	if err != nil { return err }
	if ticket.Status != "pending_approval" { return errors.New("only pending tickets can be rejected") }
	ticket.Status = "rejected"
	approvedBy := rejectedBy
	ticket.ApprovedBy = &approvedBy
	return s.repo.Update(ticket)
}

func (s *ChangeSvc) Execute(id uint64, executedBy string) error {
	ticket, err := s.repo.GetByID(id)
	if err != nil { return err }
	if ticket.Status != "approved" { return errors.New("only approved tickets can be executed") }
	now := time.Now()
	ticket.Status = "executing"
	ticket.ExecutedBy = &executedBy
	ticket.ExecutedAt = &now
	return s.repo.Update(ticket)
}

func (s *ChangeSvc) Complete(id uint64) error {
	ticket, err := s.repo.GetByID(id)
	if err != nil { return err }
	if ticket.Status != "executing" { return errors.New("only executing tickets can be completed") }
	ticket.Status = "completed"
	return s.repo.Update(ticket)
}

func (s *ChangeSvc) Rollback(id uint64) error {
	ticket, err := s.repo.GetByID(id)
	if err != nil { return err }
	if ticket.Status != "completed" && ticket.Status != "executing" {
		return errors.New("can only rollback completed or executing tickets")
	}
	ticket.Status = "rolled_back"
	return s.repo.Update(ticket)
}

func (s *ChangeSvc) Fail(id uint64) error {
	ticket, err := s.repo.GetByID(id)
	if err != nil { return err }
	if ticket.Status != "executing" { return errors.New("only executing tickets can fail") }
	ticket.Status = "failed"
	return s.repo.Update(ticket)
}

func (s *ChangeSvc) Stats() (map[string]int64, error) {
	return s.repo.CountByStatus()
}