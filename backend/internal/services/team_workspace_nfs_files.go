package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"path"
	"strings"
	"time"
)

func (s *teamService) useNFSTeamFiles() bool {
	return s.workspaceText != nil && s.workspaceText.nfsProfile == "nfs-rootless-v1"
}

func (s *teamService) listNFSWorkspaceFiles(ctx context.Context, userID, teamID int, relative string) (*TeamWorkspaceListPayload, error) {
	scope, relative, err := s.teamTextScope(ctx, userID, teamID, relative)
	if err != nil {
		return nil, err
	}
	entries, err := s.workspaceText.List(ctx, scope, relative)
	if err != nil {
		return nil, err
	}
	team, err := s.requireOwnedTeam(userID, teamID)
	if err != nil {
		return nil, err
	}
	result := &TeamWorkspaceListPayload{Path: relative, Root: teamWorkspaceDisplayRoot(team, scope.WorkspacePath), Entries: make([]TeamWorkspaceFileEntry, 0, len(entries))}
	for _, e := range entries {
		kind := "file"
		if e.IsDir {
			kind = "directory"
		}
		result.Entries = append(result.Entries, TeamWorkspaceFileEntry{Name: e.Name, Path: e.Path, Type: kind, Size: e.Size, ModifiedAt: e.ModifiedAt.UTC().Format(time.RFC3339), Previewable: !e.IsDir && isPreviewableWorkspaceFile(e.Name), Editable: !e.IsDir && workspaceTextName(e.Name) && e.Size <= textPreviewMaxBytes})
	}
	sortTeamWorkspaceEntries(result.Entries)
	return result, nil
}
func (s *teamService) previewNFSWorkspaceFile(ctx context.Context, userID, teamID int, relative string) (*TeamWorkspacePreviewPayload, error) {
	scope, relative, err := s.teamTextScope(ctx, userID, teamID, relative)
	if err != nil {
		return nil, err
	}
	if relative == "" || !isPreviewableWorkspaceFile(relative) {
		return nil, fmt.Errorf("only md, txt, and json files can be previewed")
	}
	preview, err := s.workspaceText.Preview(ctx, scope, relative)
	if err != nil {
		return nil, err
	}
	if preview.Kind != "text" {
		return nil, ErrWorkspaceTextUnsupported
	}
	return &TeamWorkspacePreviewPayload{Path: relative, Name: path.Base(relative), Content: preview.Text}, nil
}
func (s *teamService) downloadNFSWorkspaceFile(ctx context.Context, userID, teamID int, relative string) (*TeamWorkspaceDownloadPayload, error) {
	scope, relative, err := s.teamTextScope(ctx, userID, teamID, relative)
	if err != nil {
		return nil, err
	}
	if relative == "" {
		return nil, ErrWorkspaceRootOperation
	}
	reader, name, _, err := s.workspaceText.OpenDownload(ctx, scope, relative)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	contentType := "application/octet-stream"
	if strings.HasSuffix(name, ".zip") {
		contentType = "application/zip"
	}
	return &TeamWorkspaceDownloadPayload{Path: relative, Name: name, ContentType: contentType, Data: data}, nil
}
func (s *teamService) mkdirNFSWorkspace(ctx context.Context, userID, teamID int, req TeamWorkspaceFolderRequest) error {
	scope, parent, err := s.teamTextScope(ctx, userID, teamID, req.Path)
	if err != nil {
		return err
	}
	name, err := cleanWorkspaceEntryName(req.Name)
	if err != nil {
		return err
	}
	_, err = s.workspaceText.Mkdir(ctx, scope, joinTeamWorkspacePath(parent, name))
	return err
}
func (s *teamService) renameNFSWorkspace(ctx context.Context, userID, teamID int, req TeamWorkspaceRenameRequest) error {
	scope, relative, err := s.teamTextScope(ctx, userID, teamID, req.Path)
	if err != nil {
		return err
	}
	name, err := cleanWorkspaceEntryName(req.NewName)
	if err != nil {
		return err
	}
	_, err = s.workspaceText.Rename(ctx, scope, relative, joinTeamWorkspacePath(path.Dir(relative), name))
	return err
}
func (s *teamService) deleteNFSWorkspace(ctx context.Context, userID, teamID int, relative string) error {
	scope, relative, err := s.teamTextScope(ctx, userID, teamID, relative)
	if err != nil {
		return err
	}
	return s.workspaceText.Delete(ctx, scope, relative)
}
func (s *teamService) uploadNFSWorkspace(ctx context.Context, userID, teamID int, target string, files []*multipart.FileHeader, relativePaths []string) error {
	if len(files) == 0 {
		return fmt.Errorf("no files uploaded")
	}
	scope, target, err := s.teamTextScope(ctx, userID, teamID, target)
	if err != nil {
		return err
	}
	for i, header := range files {
		if header == nil {
			continue
		}
		name := header.Filename
		if i < len(relativePaths) && strings.TrimSpace(relativePaths[i]) != "" {
			name = relativePaths[i]
		}
		relative, err := cleanTeamWorkspacePath(name)
		if err != nil {
			return err
		}
		if relative == "" {
			return ErrWorkspaceFileNameInvalid
		}
		destination := joinTeamWorkspacePath(target, relative)
		directory := path.Dir(destination)
		if directory != "." && directory != "" {
			// MkdirAll is already confined to the Team grant. An existing directory is
			// usable, while a preexisting file/symlink remains an error at Upload.
			if _, err = s.workspaceText.Mkdir(ctx, scope, directory); err != nil && !errors.Is(err, ErrWorkspaceEntryExists) {
				return err
			}
		}
		input, err := header.Open()
		if err != nil {
			return err
		}
		_, uploadErr := s.workspaceText.Upload(ctx, scope, directory, path.Base(destination), input, header.Size)
		closeErr := input.Close()
		if uploadErr != nil {
			return uploadErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
