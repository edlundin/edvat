package pgext

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"github.com/edlundin/edvat/internal/baseatlas"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

type Policy struct {
	Name        string
	Schema      string
	Table       string
	Command     string
	Roles       []string
	Using       string
	Check       string
	Restrictive bool
	Comment     string
}

type PolicyState map[string]Policy

func ParsePolicyFiles(paths []string) (PolicyState, error) {
	return parseStateFiles(paths, "policy", ParsePoliciesHCL)
}

func ParsePoliciesHCL(src []byte, filename string) (PolicyState, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse policy hcl: %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parse policy hcl: unexpected body type %T", file.Body)
	}

	state := PolicyState{}
	for _, block := range body.Blocks {
		if block.Type != "policy" || len(block.Labels) != 1 {
			continue
		}
		policy := Policy{Name: block.Labels[0]}
		attrs := block.Body.Attributes
		if attr, ok := attrs["on"]; ok {
			schemaName, tableName, err := tableExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode policy.%s.on: %w", policy.Name, err)
			}
			policy.Schema = schemaName
			policy.Table = tableName
		}
		if attr, ok := attrs["for"]; ok {
			command, err := symbolOrString(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode policy.%s.for: %w", policy.Name, err)
			}
			policy.Command = strings.ToUpper(command)
		}
		if attr, ok := attrs["to"]; ok {
			roles, err := symbolOrStringList(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode policy.%s.to: %w", policy.Name, err)
			}
			policy.Roles = roles
		}
		if attr, ok := attrs["using"]; ok {
			using, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode policy.%s.using: %w", policy.Name, err)
			}
			policy.Using = strings.TrimSpace(using)
		}
		if attr, ok := attrs["check"]; ok {
			check, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode policy.%s.check: %w", policy.Name, err)
			}
			policy.Check = strings.TrimSpace(check)
		}
		if attr, ok := attrs["restrictive"]; ok {
			restrictive, err := boolExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode policy.%s.restrictive: %w", policy.Name, err)
			}
			policy.Restrictive = restrictive
		}
		if attr, ok := attrs["comment"]; ok {
			comment, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode policy.%s.comment: %w", policy.Name, err)
			}
			policy.Comment = comment
		}
		if policy.Table == "" {
			return nil, fmt.Errorf("policy.%s requires on", policy.Name)
		}
		state[policyID(policy)] = policy
	}
	return state, nil
}

func InspectPoliciesURL(ctx context.Context, url string) (PolicyState, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}
	return InspectPolicies(ctx, db)
}

func InspectPolicies(ctx context.Context, db *sql.DB) (PolicyState, error) {
	rows, err := db.QueryContext(ctx, `
SELECT p.schemaname, p.tablename, p.policyname, p.cmd, p.roles, p.qual, p.with_check, p.permissive, COALESCE(d.description, '')
FROM pg_policies p
JOIN pg_namespace n ON n.nspname = p.schemaname
JOIN pg_class c ON c.relnamespace = n.oid AND c.relname = p.tablename
JOIN pg_policy pol ON pol.polrelid = c.oid AND pol.polname = p.policyname
LEFT JOIN pg_description d ON d.objoid = pol.oid AND d.classoid = 'pg_policy'::regclass AND d.objsubid = 0
WHERE p.schemaname NOT IN ('pg_catalog', 'information_schema')
ORDER BY p.schemaname, p.tablename, p.policyname`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres policies: %w", err)
	}
	defer rows.Close()
	state := PolicyState{}
	for rows.Next() {
		var policy Policy
		var roles string
		var using, check sql.NullString
		var permissive string
		if err := rows.Scan(&policy.Schema, &policy.Table, &policy.Name, &policy.Command, &roles, &using, &check, &permissive, &policy.Comment); err != nil {
			return nil, fmt.Errorf("scan postgres policy: %w", err)
		}
		policy.Roles = parsePolicyRoles(roles)
		policy.Using = strings.TrimSpace(using.String)
		policy.Check = strings.TrimSpace(check.String)
		policy.Restrictive = strings.EqualFold(permissive, "RESTRICTIVE")
		state[policyID(policy)] = policy
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres policies: %w", err)
	}
	return state, nil
}

func DiffPolicies(current, desired PolicyState) []baseatlas.Statement {
	if current == nil {
		current = PolicyState{}
	}
	if desired == nil {
		desired = PolicyState{}
	}
	ids := stateIDs(current, desired)

	var statements []baseatlas.Statement
	for _, id := range ids {
		cur, hasCurrent := current[id]
		des, hasDesired := desired[id]
		switch {
		case !hasCurrent && hasDesired:
			statements = append(statements, createPolicyStatements(des)...)
		case hasCurrent && !hasDesired:
			statements = append(statements, dropPolicyStatement(cur))
		case hasCurrent && hasDesired:
			if !samePolicyDefinition(cur, des) {
				statements = append(statements, dropPolicyStatement(cur))
				statements = append(statements, createPolicyStatements(des)...)
				continue
			}
			if cur.Comment != des.Comment {
				statements = append(statements, baseatlas.Statement{Comment: "set comment on policy " + policyID(des), SQL: commentPolicySQL(des)})
			}
		}
	}
	return statements
}

func createPolicyStatements(policy Policy) []baseatlas.Statement {
	statements := []baseatlas.Statement{{Comment: "create policy " + policyID(policy), SQL: createPolicySQL(policy), Reverse: dropPolicyStatement(policy).SQL}}
	if policy.Comment != "" {
		statements = append(statements, baseatlas.Statement{Comment: "set comment on policy " + policyID(policy), SQL: commentPolicySQL(policy), Reverse: "COMMENT ON POLICY " + quoteIdent(policy.Name) + " ON " + qualifiedIdent(policy.Schema, policy.Table) + " IS NULL"})
	}
	return statements
}

func dropPolicyStatement(policy Policy) baseatlas.Statement {
	return baseatlas.Statement{Comment: "drop policy " + policyID(policy) + " (destructive)", SQL: "DROP POLICY " + quoteIdent(policy.Name) + " ON " + qualifiedIdent(policy.Schema, policy.Table)}
}

func createPolicySQL(policy Policy) string {
	var b strings.Builder
	b.WriteString("CREATE POLICY ")
	b.WriteString(quoteIdent(policy.Name))
	b.WriteString(" ON ")
	b.WriteString(qualifiedIdent(policy.Schema, policy.Table))
	if policy.Restrictive {
		b.WriteString(" AS RESTRICTIVE")
	}
	if policy.Command != "" {
		b.WriteString(" FOR ")
		b.WriteString(policy.Command)
	}
	if len(policy.Roles) > 0 {
		b.WriteString(" TO ")
		b.WriteString(roleList(policy.Roles))
	}
	if policy.Using != "" {
		b.WriteString(" USING (")
		b.WriteString(policy.Using)
		b.WriteString(")")
	}
	if policy.Check != "" {
		b.WriteString(" WITH CHECK (")
		b.WriteString(policy.Check)
		b.WriteString(")")
	}
	return b.String()
}

func commentPolicySQL(policy Policy) string {
	return "COMMENT ON POLICY " + quoteIdent(policy.Name) + " ON " + qualifiedIdent(policy.Schema, policy.Table) + " IS " + nullableLiteral(policy.Comment)
}

func samePolicyDefinition(a, b Policy) bool {
	return a.Command == b.Command && strings.Join(a.Roles, ",") == strings.Join(b.Roles, ",") && normalizePolicyExpression(a.Using) == normalizePolicyExpression(b.Using) && normalizePolicyExpression(a.Check) == normalizePolicyExpression(b.Check) && a.Restrictive == b.Restrictive
}

func normalizePolicyExpression(expr string) string {
	normalized := normalizeSQL(expr)
	normalized = trimBalancedParens(normalized)
	normalized = currentSettingTextCastRe.ReplaceAllString(normalized, `current_setting('$1')`)
	normalized = castedParenFunctionRe.ReplaceAllString(normalized, `$1::$2`)
	return normalized
}

func trimBalancedParens(expr string) string {
	for strings.HasPrefix(expr, "(") && strings.HasSuffix(expr, ")") && balancedParens(expr[1:len(expr)-1]) {
		expr = strings.TrimSpace(expr[1 : len(expr)-1])
	}
	return expr
}

func balancedParens(expr string) bool {
	depth := 0
	inSingle := false
	for i := 0; i < len(expr); i++ {
		ch := expr[i]
		if ch == '\'' {
			inSingle = !inSingle
			continue
		}
		if inSingle {
			continue
		}
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0
}

var (
	currentSettingTextCastRe = regexp.MustCompile(`current_setting\('([^']+)'::text\)`)
	castedParenFunctionRe    = regexp.MustCompile(`\((current_setting\('[^']+'\))\)::([a-zA-Z_][a-zA-Z0-9_]*)`)
)

func policyID(policy Policy) string { return viewID(policy.Schema, policy.Table) + "." + policy.Name }

func roleList(roles []string) string {
	parts := make([]string, 0, len(roles))
	for _, role := range roles {
		if strings.EqualFold(role, "public") {
			parts = append(parts, "PUBLIC")
			continue
		}
		parts = append(parts, quoteIdent(role))
	}
	return strings.Join(parts, ", ")
}

func parsePolicyRoles(roles string) []string {
	roles = strings.Trim(roles, "{}")
	if strings.TrimSpace(roles) == "" {
		return nil
	}
	parts := strings.Split(roles, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(strings.TrimSpace(part), `"`)
		out = append(out, part)
	}
	return out
}
