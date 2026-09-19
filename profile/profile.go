package profile

import (
	_ "embed"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed resume.yaml
var embeddedResume []byte

type Resume struct {
	Profile    Person       `yaml:"profile"`
	About      About        `yaml:"about"`
	Status     Status       `yaml:"status"`
	Log        []LogEntry   `yaml:"log"`
	Random     []RandomNote `yaml:"random"`
	Experience []Experience `yaml:"experience"`
	Skills     []SkillGroup `yaml:"skills"`
	Projects   []Project    `yaml:"projects"`
}

type Person struct {
	Name        string   `yaml:"name"`
	ChineseName string   `yaml:"chinese_name"`
	Roles       []string `yaml:"roles"`
	Tagline     string   `yaml:"tagline"`
	Location    string   `yaml:"location"`
	Email       string   `yaml:"email"`
	Website     string   `yaml:"website"`
	GitHub      string   `yaml:"github"`
	ResumeURL   string   `yaml:"resume_url"`
}

// Status 是 /status 的输入：一句话状态 + 目标岗位 + 当前关注领域。
type Status struct {
	Headline string   `yaml:"headline"`
	Roles    []string `yaml:"roles"`
	Focus    []string `yaml:"focus"`
}

// LogEntry 是 /log 的输入：最近在学、在建、在试的东西。
type LogEntry struct {
	Date string `yaml:"date"`
	Kind string `yaml:"kind"`
	Text string `yaml:"text"`
}

// RandomNote 是 /random 的输入：网站其他地方不会出现的轻量内容。
type RandomNote struct {
	Label string `yaml:"label"`
	Text  string `yaml:"text"`
}

type About struct {
	Summary    string   `yaml:"summary"`
	Principles []string `yaml:"principles"`
}

type Experience struct {
	Period      string   `yaml:"period"`
	Company     string   `yaml:"company"`
	Role        string   `yaml:"role"`
	Description string   `yaml:"description"`
	Highlights  []string `yaml:"highlights"`
}

type SkillGroup struct {
	Name  string   `yaml:"name"`
	Items []string `yaml:"items"`
}

type Project struct {
	ID           string   `yaml:"id"`
	Title        string   `yaml:"title"`
	Subtitle     string   `yaml:"subtitle"`
	Year         string   `yaml:"year"`
	Role         string   `yaml:"role"`
	Tags         []string `yaml:"tags"`
	Problem      string   `yaml:"problem"`
	Decision     string   `yaml:"decision"`
	Deliverables []string `yaml:"deliverables"`
	Result       string   `yaml:"result"`
	Link         string   `yaml:"link"`
}

func Load(path string) (Resume, error) {
	data := embeddedResume
	if strings.TrimSpace(path) != "" {
		fileData, err := os.ReadFile(path)
		if err != nil {
			return Resume{}, fmt.Errorf("read resume content: %w", err)
		}
		data = fileData
	}

	var resume Resume
	if err := yaml.Unmarshal(data, &resume); err != nil {
		return Resume{}, fmt.Errorf("parse resume content: %w", err)
	}
	if err := resume.Validate(); err != nil {
		return Resume{}, err
	}
	return resume, nil
}

func (r Resume) Validate() error {
	if strings.TrimSpace(r.Profile.Name) == "" {
		return fmt.Errorf("resume content: profile.name is required")
	}
	if len(r.Profile.Roles) == 0 {
		return fmt.Errorf("resume content: at least one profile.roles item is required")
	}

	seen := make(map[string]struct{}, len(r.Projects))
	for _, project := range r.Projects {
		id := strings.TrimSpace(project.ID)
		if id == "" || strings.TrimSpace(project.Title) == "" {
			return fmt.Errorf("resume content: every project needs an id and title")
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("resume content: duplicate project id %q", id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func (r Resume) ProjectByID(id string) (Project, bool) {
	id = strings.TrimSpace(strings.ToLower(id))
	for _, project := range r.Projects {
		if strings.ToLower(project.ID) == id {
			return project, true
		}
	}
	return Project{}, false
}
