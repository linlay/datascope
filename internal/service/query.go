// Package service wires the rewriter to the database: resolve the trusted
// user, rewrite the statement, check configured guards, execute with binds.
package service

import (
	"context"
	"database/sql"
	"time"

	"sql-rewrite-server/internal/rewrite"
	"sql-rewrite-server/internal/store"
)

// QueryService executes AI-generated SQL only after permission rewriting.
type QueryService struct {
	Store    *store.Store
	Rewriter *rewrite.Rewriter
	// AsOf is the reference time for grant expiry, bound as ${asOf}. Fixed by
	// default so the permission fixtures are reproducible.
	AsOf time.Time
}

// New builds a QueryService. It fails if the rewrite config does not cover
// exactly the database's tables.
func New(ctx context.Context, st *store.Store, cfg *rewrite.Config, asOf time.Time) (*QueryService, error) {
	names, err := st.TableNames(ctx)
	if err != nil {
		return nil, err
	}
	rw, err := rewrite.New(cfg, names)
	if err != nil {
		return nil, err
	}
	return &QueryService{Store: st, Rewriter: rw, AsOf: asOf}, nil
}

// Config returns the rewrite configuration in use.
func (s *QueryService) Config() *rewrite.Config { return s.Rewriter.Config() }

// Bindings returns the trusted values for every template variable.
func (s *QueryService) Bindings(userID int64) map[string]any {
	return map[string]any{
		rewrite.VarUserID: userID,
		rewrite.VarAsOf:   s.AsOf.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

func namedArgs(values map[string]any, names []string) []any {
	args := make([]any, 0, len(names))
	for _, name := range names {
		args = append(args, sql.Named(name, values[name]))
	}
	return args
}

// Rewritten is a rewritten statement plus the bind values for one user.
type Rewritten struct {
	*rewrite.Result
	Binds map[string]any `json:"binds"`
	args  []any
}

// Rewrite rewrites sql for userID and evaluates the configured deny_unless
// guards of every referenced table. It does not execute the statement.
func (s *QueryService) Rewrite(ctx context.Context, query string, userID int64) (*Rewritten, error) {
	res, err := s.Rewriter.Rewrite(query)
	if err != nil {
		return nil, err
	}
	values := s.Bindings(userID)
	for _, g := range res.Guards {
		ok, err := s.Store.QueryBool(ctx, rewrite.GuardQuery(g.SQL), namedArgs(values, g.Params)...)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, &rewrite.DeniedError{Reason: "无权查询表 " + g.Table + "（" + g.Message + "）"}
		}
	}
	binds := make(map[string]any, len(res.Params))
	for _, name := range res.Params {
		binds[name] = values[name]
	}
	return &Rewritten{Result: res, Binds: binds, args: namedArgs(values, res.Params)}, nil
}

// Result is a rewritten statement plus its rows.
type Result struct {
	*Rewritten
	*store.QueryResult
}

// Query rewrites sql for userID and runs the rewritten statement.
func (s *QueryService) Query(ctx context.Context, query string, userID int64, maxRows int) (*Result, error) {
	rewritten, err := s.Rewrite(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	rows, err := s.Execute(ctx, rewritten, maxRows)
	if err != nil {
		return nil, err
	}
	return &Result{Rewritten: rewritten, QueryResult: rows}, nil
}

// Execute runs an already rewritten statement with its binds.
func (s *QueryService) Execute(ctx context.Context, r *Rewritten, maxRows int) (*store.QueryResult, error) {
	return s.Store.Query(ctx, r.RewrittenSQL, maxRows, r.args...)
}
