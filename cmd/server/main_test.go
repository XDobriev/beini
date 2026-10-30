package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSafeFileServer_Security(t *testing.T) {
	// Создаем временную директорию с файлами
	tmpDir := t.TempDir()

	// Публичные файлы
	if err := os.WriteFile(filepath.Join(tmpDir, "index.html"), []byte("<h1>BEINI</h1>"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "style.css"), []byte("body { color: red; }"), 0644); err != nil {
		t.Fatal(err)
	}

	// Скрытые и служебные файлы (должны блокироваться)
	gitDir := filepath.Join(tmpDir, ".git")
	_ = os.Mkdir(gitDir, 0755)
	_ = os.WriteFile(filepath.Join(gitDir, "config"), []byte("[core]\nrepo=true"), 0644)
	_ = os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("SECRET=12345"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "iiko_config.json"), []byte(`{"apiKey":"secret"}`), 0644)

	internalDir := filepath.Join(tmpDir, "internal")
	_ = os.Mkdir(internalDir, 0755)
	_ = os.WriteFile(filepath.Join(internalDir, "code.go"), []byte("package main"), 0644)

	handler := safeFileServer(tmpDir)

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{"Root path", "/", http.StatusOK},
		{"index.html", "/index.html", http.StatusOK},
		{"style.css", "/style.css", http.StatusOK},
		{"Block .git/config", "/.git/config", http.StatusNotFound},
		{"Block .git/HEAD", "/.git/HEAD", http.StatusNotFound},
		{"Block .env", "/.env", http.StatusNotFound},
		{"Block json file", "/iiko_config.json", http.StatusNotFound},
		{"Block internal directory", "/internal/code.go", http.StatusNotFound},
		{"Missing static asset with ext", "/missing.png", http.StatusNotFound},
		{"SPA fallback without ext", "/about-restaurant", http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Errorf("Path %s got status %d; want %d", tc.path, w.Code, tc.wantStatus)
			}
		})
	}
}

