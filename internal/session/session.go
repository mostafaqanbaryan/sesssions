package session

import (
	"fmt"
	"math"
	"os"
	"path"
	"strings"

	"github.com/mostafaqanbaryan/sesssions/internal/config"
	"github.com/mostafaqanbaryan/sesssions/internal/domain"
)

type Session struct {
	config *config.Config
}

func NewSession(config *config.Config) *Session {
	return &Session{
		config: config,
	}
}

func (a *Session) getSessions(currentSessions []domain.SearchDirectoryItem) ([]domain.SearchDirectoryItem, error) {
	var items []domain.SearchDirectoryItem
	var directories []domain.SearchDirectoryItem
	items = append(items, currentSessions...)

	for _, item := range a.config.Projects {
		if strings.HasSuffix(item, "/") {
			dirs, err := os.ReadDir(item)
			if err != nil {
				return nil, err
			}

			for _, entry := range dirs {
				if entry.IsDir() {
					directories = append(directories, domain.NewSearchDirectoryItem(entry.Name(), item, domain.SearchItemKindDirectory))
				}
			}
		} else {
			entry, err := os.Stat(item)
			if err != nil {
				return nil, err
			}

			if entry.IsDir() {
				directories = append(directories, domain.NewSearchDirectoryItem(entry.Name(), path.Dir(item), domain.SearchItemKindDirectory))
			}
		}
	}

	for _, item := range directories {
		duplicate := false
		for _, session := range currentSessions {
			if session.GetFullPath() == item.GetFullPath() {
				duplicate = true
				break
			}
		}

		if !duplicate {
			items = append(items, item)
		}
	}

	return items, nil
}

func (a *Session) List(currentSessions []domain.SearchDirectoryItem) ([]string, error) {
	items, err := a.getSessions(currentSessions)
	if err != nil {
		return nil, err
	}

	maxNameLen := 0
	for _, r := range items {
		maxNameLen = max(maxNameLen, len(r.GetDirName())+8)
	}

	var rows []string
	for i, r := range items {
		spaces := strings.Repeat(" ", int(math.Round(float64(maxNameLen-len(r.GetDirName())))))
		rows = append(rows, fmt.Sprintf("%02d\t[%s]\t%s%s\t%s\t%s", i+1, r.GetKind(), r.GetDirName(), spaces, r.GetParentDir(), r.GetFullPath()))
	}

	return rows, nil
}
