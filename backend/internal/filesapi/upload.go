package filesapi

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
)

func (s *Service) upload(w http.ResponseWriter, r *http.Request) {
	file, header, err := s.multipartFile(w, r)
	if err != nil {
		server.Respond(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	defer removeMultipartTemp(r)
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, s.options.MaxUpload+1))
	if err != nil || int64(len(data)) > s.options.MaxUpload {
		server.Respond(w, http.StatusRequestEntityTooLarge, "上传文件超过大小限制", nil)
		return
	}
	ext := strings.TrimPrefix(filepath.Ext(filepath.Base(header.Filename)), ".")
	if ext == "" || len(ext) > 16 || strings.ContainsAny(ext, `/\\`) {
		server.Respond(w, http.StatusBadRequest, "上传文件缺少有效扩展名", nil)
		return
	}
	digest := md5.Sum(data)
	hash := hex.EncodeToString(digest[:])
	relative := filepath.ToSlash(filepath.Join(hash[:1], hash+"."+ext))
	target := filepath.Join(s.store.DataDir(), "files", filepath.FromSlash(relative))
	if err := writeScoped(filepath.Join(s.store.DataDir(), "files"), target, data); err != nil {
		server.Respond(w, http.StatusInternalServerError, "保存上传文件失败", nil)
		return
	}
	server.Respond(w, http.StatusOK, "上传完成", relative)
}

func (s *Service) uploadBase64(w http.ResponseWriter, r *http.Request) {
	file, _, err := s.multipartFile(w, r)
	if err != nil {
		server.Respond(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	defer removeMultipartTemp(r)
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, s.options.MaxUpload+1))
	if err != nil || int64(len(data)) > s.options.MaxUpload {
		server.Respond(w, http.StatusRequestEntityTooLarge, "上传文件超过大小限制", nil)
		return
	}
	server.Respond(w, http.StatusOK, "success", base64.StdEncoding.EncodeToString(data))
}

func (s *Service) uploadText(w http.ResponseWriter, r *http.Request) {
	file, _, err := s.multipartFile(w, r)
	if err != nil {
		server.Respond(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	defer removeMultipartTemp(r)
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, s.options.MaxUpload+1))
	if err != nil || int64(len(data)) > s.options.MaxUpload {
		server.Respond(w, http.StatusRequestEntityTooLarge, "上传文件超过大小限制", nil)
		return
	}
	server.Respond(w, http.StatusOK, "success", string(data))
}

func removeMultipartTemp(r *http.Request) {
	if r.MultipartForm != nil {
		_ = r.MultipartForm.RemoveAll()
	}
}

func (s *Service) multipartFile(w http.ResponseWriter, r *http.Request) (multipart.File, *multipart.FileHeader, error) {
	r.Body = http.MaxBytesReader(w, r.Body, s.options.MaxUpload+(1<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		removeMultipartTemp(r)
		return nil, nil, errors.New("multipart 请求格式错误")
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return nil, nil, errors.New("缺少 multipart 文件字段 file")
	}
	if header.Size > s.options.MaxUpload {
		_ = file.Close()
		return nil, nil, fmt.Errorf("上传文件超过 %d 字节限制", s.options.MaxUpload)
	}
	return file, header, nil
}
