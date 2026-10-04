package auth

import "embed"

// FS — шаги goose сервиса Auth.
//
//go:embed *.sql
var FS embed.FS
