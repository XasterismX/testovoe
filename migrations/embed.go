// Package migrations содержит SQL-миграции, встроенные в бинарник.
package migrations

import "embed"

// FS — встроенные *.sql файлы, применяются в лексикографическом порядке.
//
//go:embed *.sql
var FS embed.FS
