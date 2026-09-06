package vsphere

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
)

const (
	datastoreBrowseTimeout = 2 * time.Minute
	datastoreBrowseFileCap = 10000
)

// searchDatastoreSubFoldersTaskBody is the one deliberately hand-rolled SOAP
// operation in the read-only client. The govmomi wrappers for this method live
// in packages that also expose mutations, so keeping this body here makes the
// operation and its review surface explicit.
type searchDatastoreSubFoldersTaskBody struct {
	Req    *types.SearchDatastoreSubFolders_Task         `xml:"urn:vim25 SearchDatastoreSubFolders_Task,omitempty"`
	Res    *types.SearchDatastoreSubFolders_TaskResponse `xml:"SearchDatastoreSubFolders_TaskResponse,omitempty"`
	Fault_ *soap.Fault                                   `xml:"http://schemas.xmlsoap.org/soap/envelope/ Fault,omitempty"`
}

func (b *searchDatastoreSubFoldersTaskBody) Fault() *soap.Fault { return b.Fault_ }

func (c *Client) browseDatastoreFiles(parent context.Context, datastore string, browser types.ManagedObjectReference) ([]DatastoreFile, string, string) {
	ctx, cancel := context.WithTimeout(parent, datastoreBrowseTimeout)
	defer cancel()
	if browser.Type == "" || browser.Value == "" {
		return nil, "failed", "datastore browser reference is unavailable"
	}

	spec := &types.HostDatastoreBrowserSearchSpec{
		MatchPattern: []string{"*"},
		Query:        []types.BaseFileQuery{&types.VmDiskFileQuery{}},
		Details: &types.FileQueryFlags{
			FileSize:     true,
			Modification: true,
			FileType:     true,
		},
	}
	reqBody := searchDatastoreSubFoldersTaskBody{Req: &types.SearchDatastoreSubFolders_Task{
		This:          browser,
		DatastorePath: fmt.Sprintf("[%s]", datastore),
		SearchSpec:    spec,
	}}
	var resBody searchDatastoreSubFoldersTaskBody
	if err := c.VIM().RoundTrip(ctx, &reqBody, &resBody); err != nil {
		return nil, "failed", err.Error()
	}
	if resBody.Res == nil || resBody.Res.Returnval.Value == "" {
		return nil, "failed", "datastore browser returned no task"
	}

	collector, err := property.DefaultCollector(c.VIM()).Create(ctx)
	if err != nil {
		return nil, "failed", fmt.Sprintf("create datastore browser property collector: %v", err)
	}
	defer func() { _ = collector.Destroy(context.WithoutCancel(ctx)) }()
	taskRef := resBody.Res.Returnval
	filterSpec := types.PropertyFilterSpec{
		ObjectSet: []types.ObjectSpec{{Obj: taskRef}},
		// The Task managed object exposes the complete info value atomically;
		// watching it also avoids losing the result when a server coalesces the
		// state and result changes into one update.
		PropSet: []types.PropertySpec{{Type: "Task", PathSet: []string{"info"}}},
	}
	if _, err := collector.CreateFilter(ctx, types.CreateFilter{Spec: filterSpec}); err != nil {
		return nil, "failed", fmt.Sprintf("create datastore browser task filter: %v", err)
	}

	var state types.TaskInfoState
	var result types.AnyType
	var taskErr *types.LocalizedMethodFault
	waitOptions := &property.WaitOptions{Options: &types.WaitOptions{}}
	err = collector.WaitForUpdatesEx(ctx, waitOptions, func(updates []types.ObjectUpdate) bool {
		for _, update := range updates {
			if update.Obj != taskRef {
				continue
			}
			for _, change := range update.ChangeSet {
				switch change.Name {
				case "info":
					switch value := change.Val.(type) {
					case types.TaskInfo:
						state, result, taskErr = value.State, value.Result, value.Error
					case *types.TaskInfo:
						if value != nil {
							state, result, taskErr = value.State, value.Result, value.Error
						}
					}
				case "info.state":
					state = taskInfoState(change.Val)
				case "info.result":
					result = change.Val
				case "info.error":
					switch value := change.Val.(type) {
					case *types.LocalizedMethodFault:
						taskErr = value
					case types.LocalizedMethodFault:
						taskErr = &value
					}
				}
			}
		}
		return state == types.TaskInfoStateSuccess || state == types.TaskInfoStateError
	})
	if err != nil {
		return nil, "failed", fmt.Sprintf("wait for datastore browser task: %v", err)
	}
	if state == types.TaskInfoStateError {
		if taskErr != nil && taskErr.LocalizedMessage != "" {
			return nil, "failed", taskErr.LocalizedMessage
		}
		return nil, "failed", "datastore browser task failed"
	}

	files := datastoreFiles(datastore, result)
	sort.SliceStable(files, func(i, j int) bool {
		if !strings.EqualFold(files[i].Path, files[j].Path) {
			return strings.ToLower(files[i].Path) < strings.ToLower(files[j].Path)
		}
		if files[i].Path != files[j].Path {
			return files[i].Path < files[j].Path
		}
		return files[i].SizeBytes < files[j].SizeBytes
	})
	return files, "success", ""
}

func taskInfoState(value any) types.TaskInfoState {
	switch value := value.(type) {
	case types.TaskInfoState:
		return value
	case *types.TaskInfoState:
		if value != nil {
			return *value
		}
	}
	return ""
}

func datastoreFiles(datastore string, result types.AnyType) []DatastoreFile {
	var results []types.HostDatastoreBrowserSearchResults
	switch value := result.(type) {
	case types.ArrayOfHostDatastoreBrowserSearchResults:
		results = value.HostDatastoreBrowserSearchResults
	case *types.ArrayOfHostDatastoreBrowserSearchResults:
		if value != nil {
			results = value.HostDatastoreBrowserSearchResults
		}
	case types.HostDatastoreBrowserSearchResults:
		results = []types.HostDatastoreBrowserSearchResults{value}
	case *types.HostDatastoreBrowserSearchResults:
		if value != nil {
			results = []types.HostDatastoreBrowserSearchResults{*value}
		}
	}

	files := make([]DatastoreFile, 0, min(datastoreBrowseFileCap, len(results)))
	for _, search := range results {
		for _, file := range search.File {
			if len(files) >= datastoreBrowseFileCap {
				return files
			}
			info := file.GetFileInfo()
			if info == nil {
				continue
			}
			var modified time.Time
			if info.Modification != nil {
				modified = info.Modification.UTC()
			}
			files = append(files, DatastoreFile{
				Path:      datastoreFilePath(datastore, search.FolderPath, info.Path),
				SizeBytes: info.FileSize,
				Modified:  modified,
			})
		}
	}
	return files
}

func datastoreFilePath(datastore, folder, file string) string {
	display := strings.TrimSpace(datastore)
	folder = strings.TrimSpace(strings.ReplaceAll(folder, "\\", "/"))
	if strings.HasPrefix(folder, "[") {
		if close := strings.IndexByte(folder, ']'); close >= 0 {
			folder = folder[close+1:]
		}
	}
	folder = strings.Trim(folder, " /")
	file = strings.Trim(strings.ReplaceAll(strings.TrimSpace(file), "\\", "/"), " /")
	if strings.HasPrefix(file, "[") {
		return file
	}
	relative := file
	if folder != "" && file != "" {
		relative = folder + "/" + file
	} else if folder != "" {
		relative = folder
	}
	if relative == "" {
		return fmt.Sprintf("[%s]", display)
	}
	return fmt.Sprintf("[%s] %s", display, relative)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
