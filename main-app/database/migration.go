package database // ← отдельный пакет

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5" // внешний пакет
)

type Migration struct {
	Version int
	Name    string
	UpSQL   string
	DownSQL string
}

// Экспортируемые функции (с заглавной буквы)
func InitMigrationTable(db *pgx.Conn) error {
	query := `
        CREATE TABLE IF NOT EXISTS schema_migrations (
            version INTEGER PRIMARY KEY,
            name VARCHAR(255) NOT NULL,
            applied_at TIMESTAMPTZ DEFAULT NOW()
        );
    `

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := db.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create migrations table: %v", err)
	}

	log.Println("✓ Migration tracking table initialized")
	return nil
}

func ShowMigrationStatus(db *pgx.Conn) error {
	rows, err := db.Query(context.Background(), `
        SELECT version, name, applied_at 
        FROM schema_migrations 
        ORDER BY version
    `)
	if err != nil {
		return fmt.Errorf("failed to get migration status: %v", err)
	}
	defer rows.Close()

	log.Println("=== Migration Status ===")
	count := 0
	for rows.Next() {
		var version int
		var name string
		var appliedAt time.Time

		if err := rows.Scan(&version, &name, &appliedAt); err != nil {
			return err
		}

		log.Printf("✓ %03d: %s (applied: %s)", version, name, appliedAt.Format("2006-01-02 15:04:05"))
		count++
	}

	if count == 0 {
		log.Println("No migrations found")
	} else {
		log.Printf("Total applied migrations: %d", count)
	}

	return nil
}

func LoadMigrations(migrationsDir string) ([]Migration, error) {
	files, err := ioutil.ReadDir(migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read migrations directory: %v", err)
	}

	var migrations []Migration

	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".sql") {
			continue
		}

		// Парсим версию из имени файла
		parts := strings.Split(file.Name(), "_")
		if len(parts) < 2 {
			continue
		}

		version, err := strconv.Atoi(parts[0])
		if err != nil {
			log.Printf("Warning: could not parse version from file %s", file.Name())
			continue
		}

		content, err := ioutil.ReadFile(filepath.Join(migrationsDir, file.Name()))
		if err != nil {
			return nil, fmt.Errorf("failed to read migration file %s: %v", file.Name(), err)
		}

		migration := ParseMigrationContent(string(content))
		migration.Version = version
		migration.Name = strings.TrimSuffix(file.Name(), ".sql")

		migrations = append(migrations, migration)
	}

	// Сортируем по версии
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}

func ParseMigrationContent(content string) Migration {
	var migration Migration

	upIndex := strings.Index(content, "-- +migrate Up")
	downIndex := strings.Index(content, "-- +migrate Down")

	if upIndex != -1 {
		if downIndex != -1 && downIndex > upIndex {
			migration.UpSQL = strings.TrimSpace(content[upIndex+len("-- +migrate Up") : downIndex])
			migration.DownSQL = strings.TrimSpace(content[downIndex+len("-- +migrate Down"):])
		} else {
			migration.UpSQL = strings.TrimSpace(content[upIndex+len("-- +migrate Up"):])
		}
	}

	return migration
}

func ApplyMigrations(db *pgx.Conn, migrations []Migration) error {
	ctx := context.Background()

	// Получаем список уже примененных миграций
	appliedVersions, err := getAppliedVersions(db)
	if err != nil {
		return err
	}

	appliedCount := 0

	for _, migration := range migrations {
		if contains(appliedVersions, migration.Version) {
			log.Printf("✓ Migration %03d (%s) already applied", migration.Version, migration.Name)
			continue
		}

		log.Printf("⏳ Applying migration %03d: %s", migration.Version, migration.Name)

		tx, err := db.Begin(ctx)
		if err != nil {
			return fmt.Errorf("failed to start transaction: %v", err)
		}

		start := time.Now()

		if _, err := tx.Exec(ctx, migration.UpSQL); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("failed to apply migration %d: %v", migration.Version, err)
		}

		if _, err := tx.Exec(ctx,
			"INSERT INTO schema_migrations (version, name) VALUES ($1, $2)",
			migration.Version, migration.Name); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("failed to record migration: %v", err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("failed to commit migration: %v", err)
		}

		duration := time.Since(start)
		log.Printf("✅ Migration %03d applied successfully in %v", migration.Version, duration)
		appliedCount++
	}

	if appliedCount == 0 {
		log.Println("✓ All migrations are up to date")
	} else {
		log.Printf("✅ Applied %d new migrations successfully", appliedCount)
	}

	return nil
}

// Вспомогательные функции (не экспортируемые)
func getAppliedVersions(db *pgx.Conn) ([]int, error) {
	rows, err := db.Query(context.Background(), "SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, fmt.Errorf("failed to get applied migrations: %v", err)
	}
	defer rows.Close()

	var versions []int
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}

	return versions, nil
}

func contains(slice []int, item int) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
