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

func TestSearchYouTube_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows: requires shell scripts")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "fake-ytdlp")

	content := `#!/bin/sh
cat <<'EOF'
{"id":"result1","title":"Song 1","uploader":"Artist 1","duration":180.0,"thumbnail":"https://img.example.com/1.jpg","webpage_url":"https://www.youtube.com/watch?v=result1"}
{"id":"result2","title":"Song 2","uploader":"Artist 2","duration":240.0,"thumbnail":"https://img.example.com/2.jpg","webpage_url":"https://www.youtube.com/watch?v=result2"}
{"id":"result3","title":"Song 3","uploader":"Artist 3","duration":200.0,"thumbnail":"https://img.example.com/3.jpg","webpage_url":"https://www.youtube.com/watch?v=result3"}
EOF
`
	if err := os.WriteFile(script, []byte(content), 0755); err != nil {
		t.Fatalf("failed to write fake script: %v", err)
	}

	svc := NewYTDLPService(script)
	results, err := svc.SearchYouTube(context.Background(), "test query", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}

	if results[0].ID != "result1" {
		t.Errorf("expected first result ID 'result1', got '%s'", results[0].ID)
	}
	if results[0].Title != "Song 1" {
		t.Errorf("expected first result title 'Song 1', got '%s'", results[0].Title)
	}
	if results[0].Artist != "Artist 1" {
		t.Errorf("expected first result artist 'Artist 1', got '%s'", results[0].Artist)
	}
	if results[0].Duration != 180 {
		t.Errorf("expected first result duration 180, got %d", results[0].Duration)
	}
}

func TestSearchYouTube_EmptyResults(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "fake-ytdlp")
	content := "#!/bin/sh\necho ''\n"
	os.WriteFile(script, []byte(content), 0755)

	svc := NewYTDLPService(script)
	results, err := svc.SearchYouTube(context.Background(), "no results", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearchYouTube_BinaryNotFound(t *testing.T) {
	svc := NewYTDLPService("/nonexistent/path/to/ytdlp")
	_, err := svc.SearchYouTube(context.Background(), "query", 5)
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}
