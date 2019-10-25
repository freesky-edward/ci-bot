package cibot

import (
	"gitee.com/openeuler/go-gitee/gitee"
	"github.com/golang/glog"
)

// HandleIssueEvent handles issue event
func (s *Server) HandleIssueEvent(event *gitee.IssueEvent) {
	if event == nil {
		return
	}

	// handle events
	switch *event.Action {
	case "open":
		glog.Info("received a issue open event")
	}
}