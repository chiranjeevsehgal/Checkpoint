package service

import (
	"context"
	"errors"
	"testing"

	"checkpoint/ingestion/internal/domain"
)

type fakeUserSettings struct {
	languages []string
	saved     []string
	err       error
}

func (f *fakeUserSettings) GetLanguages(_ context.Context, _ string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.languages, nil
}

func (f *fakeUserSettings) SetLanguages(_ context.Context, _ string, languages []string) error {
	if f.err != nil {
		return f.err
	}
	f.saved = languages
	return nil
}

func TestSettingsGetReturnsCatalog(t *testing.T) {
	repo := &fakeUserSettings{languages: []string{"eng"}}
	svc := NewSettingsService(repo)

	got, err := svc.Get(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Languages) != 1 || got.Languages[0] != "eng" {
		t.Fatalf("languages: got %v, want [eng]", got.Languages)
	}
	if len(got.Available) != len(domain.SupportedLanguages) {
		t.Fatalf("catalog size: got %d, want %d", len(got.Available), len(domain.SupportedLanguages))
	}
}

func TestSettingsSetNormalizesAndPersists(t *testing.T) {
	repo := &fakeUserSettings{}
	svc := NewSettingsService(repo)

	got, err := svc.SetLanguages(context.Background(), "user-1", []string{" ENG ", "hin", "eng"})
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if len(got) != 2 || got[0] != "eng" || got[1] != "hin" {
		t.Fatalf("returned %v, want [eng hin]", got)
	}
	if len(repo.saved) != 2 || repo.saved[0] != "eng" || repo.saved[1] != "hin" {
		t.Fatalf("persisted %v, want [eng hin]", repo.saved)
	}
}

func TestSettingsSetRejectsUnknownCode(t *testing.T) {
	repo := &fakeUserSettings{}
	svc := NewSettingsService(repo)

	if _, err := svc.SetLanguages(context.Background(), "user-1", []string{"zzz"}); !errors.Is(err, domain.ErrInvalidLanguage) {
		t.Fatalf("want ErrInvalidLanguage, got %v", err)
	}
	if repo.saved != nil {
		t.Fatalf("invalid selection must not persist, saved %v", repo.saved)
	}
}

func TestSettingsGetPropagatesRepositoryError(t *testing.T) {
	repo := &fakeUserSettings{err: errors.New("db down")}
	svc := NewSettingsService(repo)

	if _, err := svc.Get(context.Background(), "user-1"); err == nil {
		t.Fatal("expected repository error")
	}
}
