package seeds

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIndustryNamesList(t *testing.T) {
	require.Len(t, industryNames, 27)

	seen := make(map[string]struct{}, len(industryNames))
	for _, name := range industryNames {
		require.NotEmpty(t, name)
		require.Equal(t, name, strings.TrimSpace(name), "industry name %q has leading/trailing whitespace", name)

		_, duplicate := seen[name]
		require.False(t, duplicate, "duplicate industry name %q", name)
		seen[name] = struct{}{}
	}

	require.Contains(t, industryNames, "Other")
}

func TestIndustriesSeed_Run(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	seed := NewIndustriesSeed(pool)
	require.NoError(t, seed.Run(ctx))

	t.Cleanup(func() {
		require.NoError(t, seed.Run(ctx))
	})

	t.Run("inserts every industry", func(t *testing.T) {
		names := selectIndustryNames(t, pool, industryNames)
		assert.ElementsMatch(t, industryNames, names)
	})

	t.Run("is idempotent across runs", func(t *testing.T) {
		before := countIndustries(t, pool, industryNames)

		require.NoError(t, seed.Run(ctx))
		require.NoError(t, seed.Run(ctx))

		after := countIndustries(t, pool, industryNames)
		assert.Equal(t, before, after)

		var duplicates int
		err := pool.QueryRow(ctx, `
			SELECT count(*) FROM (
				SELECT name FROM industries WHERE name = ANY($1) GROUP BY name HAVING count(*) > 1
			) d`, industryNames).Scan(&duplicates)
		require.NoError(t, err)
		assert.Zero(t, duplicates)
	})

	t.Run("refreshes updated_at on conflict", func(t *testing.T) {
		before := industryUpdatedAt(t, pool, "Retail")

		time.Sleep(10 * time.Millisecond)
		require.NoError(t, seed.Run(ctx))

		after := industryUpdatedAt(t, pool, "Retail")
		assert.True(t, after.After(before), "expected updated_at to advance, before=%v after=%v", before, after)
	})

	t.Run("leaves rows outside the list untouched", func(t *testing.T) {
		sentinel := "ZZ Seed Test " + randomSuffix(t)

		_, err := pool.Exec(ctx, `INSERT INTO industries (name) VALUES ($1)`, sentinel)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, err := pool.Exec(ctx, `DELETE FROM industries WHERE name = $1`, sentinel)
			require.NoError(t, err)
		})

		require.NoError(t, seed.Run(ctx))

		var count int
		err = pool.QueryRow(ctx, `SELECT count(*) FROM industries WHERE name = $1`, sentinel).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count)
	})
}

func selectIndustryNames(t *testing.T, pool *pgxpool.Pool, names []string) []string {
	t.Helper()

	rows, err := pool.Query(context.Background(), `SELECT name FROM industries WHERE name = ANY($1)`, names)
	require.NoError(t, err)
	defer rows.Close()

	var result []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		result = append(result, name)
	}
	require.NoError(t, rows.Err())

	return result
}

func countIndustries(t *testing.T, pool *pgxpool.Pool, names []string) int {
	t.Helper()

	var count int
	err := pool.QueryRow(context.Background(), `SELECT count(*) FROM industries WHERE name = ANY($1)`, names).Scan(&count)
	require.NoError(t, err)

	return count
}

func industryUpdatedAt(t *testing.T, pool *pgxpool.Pool, name string) time.Time {
	t.Helper()

	var updatedAt time.Time
	err := pool.QueryRow(context.Background(), `SELECT updated_at FROM industries WHERE name = $1`, name).Scan(&updatedAt)
	require.NoError(t, err)

	return updatedAt
}
