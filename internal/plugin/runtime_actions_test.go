package plugin

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRuntimeActionsStoreFilesInPluginDataDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv(pluginDataDirEnv, root)
	actions := runtimeActions{}
	ctx := context.Background()
	image := base64.StdEncoding.EncodeToString([]byte{0x89, 'P', 'N', 'G'})

	for relative, write := range map[string]func() error{
		"guides/march/index.json":  func() error { _, err := actions.FileWriteText(ctx, "guides/march/index.json", "{}\n"); return err },
		"guides/march/01.png":      func() error { _, err := actions.FileWriteBase64(ctx, "guides/march/01.png", image); return err },
		"guides/march7/index.json": func() error { _, err := actions.FileWriteText(ctx, "guides/march7/index.json", "{}\n"); return err },
	} {
		if err := write(); err != nil {
			t.Fatalf("write %s: %v", relative, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "guides", "march", "index.json")); err != nil {
		t.Fatalf("text file was not written inside the data directory: %v", err)
	}

	if text, err := actions.FileRead(ctx, "guides/march/index.json"); err != nil || text["exists"] != true || text["content_text"] != "{}\n" {
		t.Fatalf("read text = %#v, %v", text, err)
	}
	if binary, err := actions.FileRead(ctx, "guides/march/01.png"); err != nil || binary["content_base64"] != image {
		t.Fatalf("read binary = %#v, %v", binary, err)
	}
	if missing, err := actions.FileRead(ctx, "guides/missing.json"); err != nil || missing["exists"] != false {
		t.Fatalf("read missing = %#v, %v", missing, err)
	}
	listed, err := actions.FileList(ctx, "guides/march/")
	if err != nil || !reflect.DeepEqual(listed["paths"], []string{"guides/march/01.png", "guides/march/index.json"}) {
		t.Fatalf("list = %#v, %v", listed, err)
	}
	if _, err := actions.FileRead(ctx, "../outside.json"); err == nil {
		t.Fatal("a path outside the data directory was accepted")
	}
}
