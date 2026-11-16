package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// Plane is the part of the Plane REST API (v1) that the relay uses.
type Plane struct {
	URL       string // https://plane.example.com, no trailing slash
	Workspace string
	Project   string // project UUID
	Token     string
	HTTP      *http.Client
}

// Item is a work item as the relay needs it.
type Item struct {
	ID       string `json:"id"`
	Sequence int    `json:"sequence_id"`
	State    string `json:"state"`
}

func (p *Plane) base() string {
	return fmt.Sprintf("%s/api/v1/workspaces/%s/projects/%s", p.URL, p.Workspace, p.Project)
}

func (p *Plane) do(method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, p.base()+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", p.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("plane %s %s: %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (p *Plane) client() *http.Client {
	if p.HTTP != nil {
		return p.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

// named lists the id and name of each state or label.
func (p *Plane) named(path string) (map[string]string, error) {
	var page struct {
		Results []struct{ ID, Name string } `json:"results"`
	}
	if err := p.do("GET", path, nil, &page); err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, r := range page.Results {
		m[r.Name] = r.ID
	}
	return m, nil
}

// Labels maps label names to ids.
func (p *Plane) Labels() (map[string]string, error) { return p.named("/labels/") }

// States maps state names to ids.
func (p *Plane) States() (map[string]string, error) { return p.named("/states/") }

// Create files a new work item.
func (p *Plane) Create(title, html string, labels []string) (Item, error) {
	var it Item
	err := p.do("POST", "/work-items/", map[string]any{"name": title, "description_html": html, "labels": labels}, &it)
	return it, err
}

// Get reads a work item.
func (p *Plane) Get(id string) (Item, error) {
	var it Item
	err := p.do("GET", "/work-items/"+id+"/", nil, &it)
	return it, err
}

// Comment adds a comment to a work item.
func (p *Plane) Comment(id, html string) error {
	return p.do("POST", "/work-items/"+id+"/comments/", map[string]any{"comment_html": html}, nil)
}

// Attach uploads a file to a work item: Plane returns a presigned form for
// the object store, the relay posts the file to it, then confirms.
func (p *Plane) Attach(id, name, mime string, data []byte) error {
	var up struct {
		AssetID    string `json:"asset_id"`
		UploadData struct {
			URL    string            `json:"url"`
			Fields map[string]string `json:"fields"`
		} `json:"upload_data"`
	}
	if err := p.do("POST", "/work-items/"+id+"/attachments/", map[string]any{"name": name, "type": mime, "size": len(data)}, &up); err != nil {
		return err
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range up.UploadData.Fields {
		_ = mw.WriteField(k, v)
	}
	fw, err := mw.CreateFormFile("file", name)
	if err != nil {
		return err
	}
	_, _ = fw.Write(data)
	_ = mw.Close()
	resp, err := p.client().Post(up.UploadData.URL, mw.FormDataContentType(), &buf)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("upload %s: %d", name, resp.StatusCode)
	}
	return p.do("PATCH", "/work-items/"+id+"/attachments/"+up.AssetID+"/", map[string]any{"is_uploaded": true}, nil)
}
