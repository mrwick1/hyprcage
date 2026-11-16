//go:build linux

package mcpserver

import (
	"fmt"
	"github.com/hexadecimil/offstage/internal/registry"
	"github.com/hexadecimil/offstage/internal/session"
	"github.com/hexadecimil/offstage/internal/task"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type taskIn struct {
	TaskID string `json:"task_id"`
}
type taskLaunchIn struct {
	TaskID  string            `json:"task_id"`
	Command []string          `json:"command" jsonschema:"argv of a non-graphical server or test process; no shell is added"`
	Cwd     string            `json:"cwd,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

func (s *Server) registerTasks(srv *mcp.Server) {
	tool(s, srv, "task_create", "Create a task that owns server processes, screens and local artifacts. Linux only. Close it with task_close when done.", s.taskCreate)
	tool(s, srv, "task_inspect", "Inspect an owned task, its resources, exit results and artifact paths. Missing sources are not proof of completion.", s.taskInspect)
	tool(s, srv, "task_launch", "Launch a non-graphical process owned by a task. Requires Linux user systemd. Captures redacted output (1 MiB per process; overlong lines are discarded). Use app_launch for graphical apps.", s.taskLaunch)
	tool(s, srv, "task_close", "Close an owned task's processes and exact screen instances. Retains local artifacts. Returns cleanup failures for retry.", s.taskClose)
}
func (s *Server) taskCreate(in struct{}) (*mcp.CallToolResult, error) {
	t, e := task.Create(session.Current().Owner())
	if e != nil {
		return nil, e
	}
	return textResult(t), nil
}
func (s *Server) taskInspect(in taskIn) (*mcp.CallToolResult, error) {
	t, e := task.Inspect(in.TaskID, session.Current().Owner())
	if e != nil {
		return nil, e
	}
	return textResult(t), nil
}
func (s *Server) taskLaunch(in taskLaunchIn) (*mcp.CallToolResult, error) {
	r, e := task.Launch(in.TaskID, session.Current().Owner(), in.Command, in.Cwd, in.Env)
	if e != nil {
		return nil, e
	}
	return textResult(r), nil
}
func (s *Server) taskClose(in taskIn) (*mcp.CallToolResult, error) {
	e := task.Close(in.TaskID, session.Current().Owner(), func(rec *registry.Screen) error { _, err := s.screenDestroy(screenIn{Screen: rec.Name}); return err })
	if e != nil {
		return nil, e
	}
	return s.taskInspect(in)
}
func validateTask(taskID string) error {
	if taskID == "" {
		return nil
	}
	t, e := task.Inspect(taskID, session.Current().Owner())
	if e != nil {
		return e
	}
	if t.State != "active" {
		return fmt.Errorf("task is %s", t.State)
	}
	return nil
}
func (s *Server) attachTask(taskID string, rec *registry.Screen) error {
	if taskID == "" {
		return nil
	}
	if err := task.Attach(taskID, session.Current().Owner(), rec); err != nil {
		_, cleanup := s.screenDestroy(screenIn{Screen: rec.Name})
		if cleanup != nil {
			return fmt.Errorf("attach task: %v; screen %s cleanup: %w", err, rec.Name, cleanup)
		}
		return err
	}
	return nil
}
