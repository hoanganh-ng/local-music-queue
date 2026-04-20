package youtube

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFetchMetadata_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows: requires shell scripts")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "fake-ytdlp")

	content := `#!/bin/sh
cat <<'EOF'
{"id":"dQw4w9WgXcQ","title":"Test Video","uploader":"Test Artist","duration":212.0,"thumbnail":"https://img.example.com/thumb.jpg","webpage_url":"https://www.youtube.com/watch?v=dQw4w9WgXcQ"}
EOF
`
	if err := os.WriteFile(script, []byte(content), 0755); err != nil {
		t.Fatalf("failed to write fake script: %v", err)
	}

	svc := NewYTDLPService(script)
	song, err := svc.FetchMetadata(context.Background(), "https://youtube.com/watch?v=test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if song.ID != "dQw4w9WgXcQ" {
		t.Errorf("expected ID 'dQw4w9WgXcQ', got '%s'", song.ID)
	}
	if song.Title != "Test Video" {
		t.Errorf("expected title 'Test Video', got '%s'", song.Title)
	}
	if song.Artist != "Test Artist" {
		t.Errorf("expected artist 'Test Artist', got '%s'", song.Artist)
	}
	if song.Thumbnail != "https://img.example.com/thumb.jpg" {
		t.Errorf("unexpected thumbnail: %s", song.Thumbnail)
	}
	if song.URL != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
		t.Errorf("unexpected URL: %s", song.URL)
	}
	if song.Duration == 0 {
		t.Error("expected non-zero duration")
	}
}

func TestFetchMetadata_InvalidJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "fake-ytdlp")
	content := "#!/bin/sh\necho 'not json'\n"
	os.WriteFile(script, []byte(content), 0755)

	svc := NewYTDLPService(script)
	_, err := svc.FetchMetadata(context.Background(), "url")
	if err == nil {
		t.Fatal("expected error for invalid JSON output")
	}
}

func TestFetchMetadata_BinaryNotFound(t *testing.T) {
	svc := NewYTDLPService("/nonexistent/path/to/ytdlp")
	_, err := svc.FetchMetadata(context.Background(), "url")
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestFetchMetadata_NonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "fake-ytdlp")
	content := "#!/bin/sh\necho 'error: video not found' >&2\nexit 1\n"
	os.WriteFile(script, []byte(content), 0755)

	svc := NewYTDLPService(script)
	_, err := svc.FetchMetadata(context.Background(), "url")
	if err == nil {
		t.Fatal("expected error for non-zero exit code")
	}
}
