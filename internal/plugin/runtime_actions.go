package plugin

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const pluginDataDirEnv = "RAYLEABOT_PLUGIN_DATA_DIR"

// runtimeActions serves the HTTP and file operations that plugin protocol v4
// removed from the host on top of the SDK actions. Files keep the result shape
// of the retired storage.file action.
type runtimeActions struct {
	*rayleabot.Actions
}

func (runtimeActions) HTTPRequest(ctx context.Context, request httpRequest) (rayleabot.ActionResult, error) {
	return fetchHTTP(ctx, request)
}

func (runtimeActions) FileRead(_ context.Context, relative string) (rayleabot.ActionResult, error) {
	target, err := pluginDataPath(relative)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(target)
	if errors.Is(err, fs.ErrNotExist) {
		return rayleabot.ActionResult{"path": relative, "exists": false}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read plugin file: %w", err)
	}
	result := rayleabot.ActionResult{"path": relative, "exists": true}
	if utf8.Valid(content) {
		result["content_text"] = string(content)
	} else {
		result["content_base64"] = base64.StdEncoding.EncodeToString(content)
	}
	return result, nil
}

func (runtimeActions) FileWriteText(_ context.Context, relative, content string) (rayleabot.ActionResult, error) {
	return writePluginFile(relative, []byte(content))
}

func (runtimeActions) FileWriteBase64(_ context.Context, relative, content string) (rayleabot.ActionResult, error) {
	decoded, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return nil, fmt.Errorf("decode plugin file content: %w", err)
	}
	return writePluginFile(relative, decoded)
}

func (runtimeActions) FileList(_ context.Context, prefix string) (rayleabot.ActionResult, error) {
	root, err := pluginDataRoot()
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0)
	err = filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		if relative = filepath.ToSlash(relative); strings.HasPrefix(relative, prefix) {
			paths = append(paths, relative)
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("list plugin files: %w", err)
	}
	sort.Strings(paths)
	return rayleabot.ActionResult{"paths": paths}, nil
}

// pluginDataRoot is the directory the host creates for this plugin; the retired
// storage.file action used the same directory, so existing caches stay readable.
func pluginDataRoot() (string, error) {
	root := strings.TrimSpace(os.Getenv(pluginDataDirEnv))
	if root == "" {
		return "", errors.New("plugin data directory is unavailable")
	}
	return root, nil
}

func pluginDataPath(relative string) (string, error) {
	root, err := pluginDataRoot()
	if err != nil {
		return "", err
	}
	local := filepath.FromSlash(relative)
	if !filepath.IsLocal(local) {
		return "", fmt.Errorf("plugin data path %q is invalid", relative)
	}
	return filepath.Join(root, local), nil
}

func writePluginFile(relative string, content []byte) (rayleabot.ActionResult, error) {
	target, err := pluginDataPath(relative)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return nil, fmt.Errorf("create plugin file directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".write-*")
	if err != nil {
		return nil, fmt.Errorf("create plugin file: %w", err)
	}
	defer func() { _ = os.Remove(temporary.Name()) }()
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return nil, fmt.Errorf("write plugin file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return nil, fmt.Errorf("write plugin file: %w", err)
	}
	if err := os.Rename(temporary.Name(), target); err != nil {
		return nil, fmt.Errorf("replace plugin file: %w", err)
	}
	return rayleabot.ActionResult{"path": relative}, nil
}
