package template

import "context"

// TemplateDetail is a template with build history for detail views.
type TemplateDetail struct {
	Record
	Builtin bool
	Builds  []BuildSummary
}

// Get returns template detail including build history.
func (s *Service) Get(ctx context.Context, templateID string) (TemplateDetail, error) {
	rec, err := s.store.GetByID(ctx, templateID)
	if err != nil {
		return TemplateDetail{}, err
	}
	builds, err := s.store.ListBuilds(ctx, templateID)
	if err != nil {
		return TemplateDetail{}, err
	}
	return TemplateDetail{
		Record:  rec,
		Builtin: IsBuiltin(rec.Namespace, rec.Name, rec.CreatedBy),
		Builds:  builds,
	}, nil
}

// Update patches a user template.
func (s *Service) Update(ctx context.Context, templateID string, req UpdateTemplateRequest) (Record, error) {
	if err := req.Validate(); err != nil {
		return Record{}, err
	}
	if err := s.store.UpdateTemplate(ctx, templateID, req); err != nil {
		return Record{}, err
	}
	return s.store.GetByID(ctx, templateID)
}

// Delete removes a user template.
func (s *Service) Delete(ctx context.Context, templateID string) error {
	return s.store.DeleteTemplate(ctx, templateID)
}
