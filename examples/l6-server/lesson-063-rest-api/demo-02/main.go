package main

import (
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"strings"
)

type fieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type apiError struct {
	Code    string       `json:"code"`
	Message string       `json:"message"`
	Details []fieldError `json:"details,omitempty"`
}

func writeError(w http.ResponseWriter, status int, e apiError) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(e)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

var emailRe = regexp.MustCompile(`^[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}$`)

type createReq struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Age   int    `json:"age"`
}

func (r createReq) validate() []fieldError {
	var errs []fieldError
	if len(strings.TrimSpace(r.Name)) < 2 {
		errs = append(errs, fieldError{Field: "name", Message: "至少 2 个字符"})
	}
	if !emailRe.MatchString(r.Email) {
		errs = append(errs, fieldError{Field: "email", Message: "不是合法邮箱"})
	}
	if r.Age < 0 || r.Age > 150 {
		errs = append(errs, fieldError{Field: "age", Message: "需在 0 到 150 之间"})
	}
	return errs
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/users", func(w http.ResponseWriter, r *http.Request) {
		var req createReq
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields() // 未知字段直接拒绝，避免静默吞掉拼写错误
		if err := dec.Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, apiError{
				Code: "INVALID_JSON", Message: "请求体解析失败",
			})
			return
		}
		if errs := req.validate(); len(errs) > 0 {
			writeError(w, http.StatusUnprocessableEntity, apiError{
				Code: "VALIDATION_FAILED", Message: "参数校验失败", Details: errs,
			})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"data": req})
	})
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
