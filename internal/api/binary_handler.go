package api

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/n8n-io/n8n-turbo/internal/binarydata"
)

func (s *Server) handleUploadBinaryData(w http.ResponseWriter, r *http.Request) {
	if s.binaryStore == nil {
		writeError(w, http.StatusServiceUnavailable, "binary data store is not configured")
		return
	}
	reader, fileName, mimeType, cleanup, err := uploadReader(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer cleanup()
	ref, err := s.binaryStore.Put(r.Context(), mimeType, fileName, reader)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": binarydata.BinaryFromRef(ref)})
}

func (s *Server) handleDownloadBinaryData(w http.ResponseWriter, r *http.Request) {
	if s.binaryStore == nil {
		writeError(w, http.StatusServiceUnavailable, "binary data store is not configured")
		return
	}
	id := chi.URLParam(r, "id")
	ref, err := s.binaryStore.Stat(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "binary data not found")
		return
	}
	reader, err := s.binaryStore.Open(r.Context(), ref)
	if err != nil {
		writeError(w, http.StatusNotFound, "binary data not found")
		return
	}
	defer reader.Close()
	mimeType := ref.MimeType
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mimeType)
	if ref.FileName != "" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(ref.FileName)))
	}
	if ref.FileSize > 0 {
		w.Header().Set("Content-Length", fmt.Sprint(ref.FileSize))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, reader)
}

func (s *Server) handleDeleteBinaryData(w http.ResponseWriter, r *http.Request) {
	if s.binaryStore == nil {
		writeError(w, http.StatusServiceUnavailable, "binary data store is not configured")
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.binaryStore.Delete(r.Context(), binarydata.Ref{ID: id}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"deleted": true, "id": id}})
}

func uploadReader(r *http.Request) (io.Reader, string, string, func(), error) {
	cleanup := func() {}
	contentType := r.Header.Get("Content-Type")
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if strings.HasPrefix(mediaType, "multipart/") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			return nil, "", "", cleanup, fmt.Errorf("invalid multipart upload: %w", err)
		}
		cleanup = func() {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
		}
		file, header, err := r.FormFile("file")
		if err != nil && r.MultipartForm != nil {
			for _, headers := range r.MultipartForm.File {
				if len(headers) == 0 {
					continue
				}
				header = headers[0]
				file, err = header.Open()
				break
			}
		}
		if err != nil {
			cleanup()
			return nil, "", "", func() {}, fmt.Errorf("missing multipart file")
		}
		cleanup = func() {
			_ = file.Close()
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
		}
		fileName := header.Filename
		mimeType := header.Header.Get("Content-Type")
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		return file, fileName, mimeType, cleanup, nil
	}
	fileName := firstNonEmpty(r.URL.Query().Get("fileName"), r.Header.Get("X-File-Name"), "binary-data")
	mimeType := mediaType
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return r.Body, fileName, mimeType, cleanup, nil
}
