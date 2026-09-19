package profile

import "testing"

func TestLoadEmbeddedResume(t *testing.T) {
	resume, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if resume.Profile.Name != "ZHANG" {
		t.Fatalf("Profile.Name = %q, want ZHANG", resume.Profile.Name)
	}
	if len(resume.Projects) < 3 {
		t.Fatalf("len(Projects) = %d, want at least 3", len(resume.Projects))
	}
}

func TestProjectByID(t *testing.T) {
	resume, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	project, ok := resume.ProjectByID("02")
	if !ok || project.Title == "" {
		t.Fatalf("ProjectByID(02) = %#v, %v", project, ok)
	}
}
