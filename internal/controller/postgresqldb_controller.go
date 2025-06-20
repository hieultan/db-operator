package controllers

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepostgres "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/github"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	postgresv1alpha1 "github.com/nakamasato/mysql-operator/api/postgresql/v1alpha1"
	pginternal "github.com/nakamasato/mysql-operator/internal/postgres"
)

const (
	postgresqlDBFinalizer                        = "postgresqldb.nakamasato.com/finalizer"
	postgresqlDBPhaseNotReady                    = "NotReady"
	postgresqlDBReasonPostgreSQLFetchFailed      = "Failed to fetch PostgreSQL"
	postgresqlDBReasonPostgreSQLConnectionFailed = "Failed to connect to postgresql"
	postgresqlDBPhaseReady                       = "Ready"
	postgresqlDBReasonCompleted                  = "Database successfully created"
)

// PostgreSQLDBReconciler reconciles a PostgreSQLDB object
type PostgreSQLDBReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	PostgreSQLClients pginternal.PostgreSQLClients
}

//+kubebuilder:rbac:groups=postgres.nakamasato.com,resources=postgresqldbs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=postgres.nakamasato.com,resources=postgresqldbs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=postgres.nakamasato.com,resources=postgresqldbs/finalizers,verbs=update

func (r *PostgreSQLDBReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithName("PostgreSQLDBReconciler")
	pgdb := &postgresv1alpha1.PostgreSQLDB{}
	err := r.Get(ctx, req.NamespacedName, pgdb)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("PostgreSQLDB not found", "req.NamespacedName", req.NamespacedName)
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get PostgreSQLDB")
		return ctrl.Result{}, err
	}

	pg := &postgresv1alpha1.PostgreSQL{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: pgdb.Spec.PostgresqlName}, pg); err != nil {
		log.Error(err, "Failed to fetch PostgreSQL")
		pgdb.Status.Phase = postgresqlDBPhaseNotReady
		pgdb.Status.Reason = postgresqlDBReasonPostgreSQLFetchFailed
		if serr := r.Status().Update(ctx, pgdb); serr != nil {
			log.Error(serr, "Failed to update PostgreSQLDB status")
		}
		return ctrl.Result{}, err
	}

	pgClient, err := r.PostgreSQLClients.GetClient(pg.GetKey())
	if err != nil {
		log.Error(err, "Failed to get PostgreSQL client", "key", pgdb.GetKey())
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	if !pgdb.GetDeletionTimestamp().IsZero() {
		if controllerutil.ContainsFinalizer(pgdb, postgresqlDBFinalizer) {
			if err := r.finalizePostgreSQLDB(ctx, pgClient, pgdb); err != nil {
				return ctrl.Result{}, err
			}
			if controllerutil.RemoveFinalizer(pgdb, postgresqlDBFinalizer) {
				if err := r.Update(ctx, pgdb); err != nil {
					return ctrl.Result{}, err
				}
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, nil
	}

	if controllerutil.AddFinalizer(pgdb, postgresqlDBFinalizer) {
		if err := r.Update(ctx, pgdb); err != nil {
			return ctrl.Result{}, err
		}
	}

	_, err = pgClient.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s", pqQuoteIdentifier(pgdb.Spec.DBName)))
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		pgdb.Status.Phase = postgresqlDBPhaseNotReady
		pgdb.Status.Reason = err.Error()
		if serr := r.Status().Update(ctx, pgdb); serr != nil {
			log.Error(serr, "Failed to update PostgreSQLDB status")
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	if err == nil {
		pgdb.Status.Phase = postgresqlDBPhaseReady
		pgdb.Status.Reason = postgresqlDBReasonCompleted
		if serr := r.Status().Update(ctx, pgdb); serr != nil {
			log.Error(serr, "Failed to update PostgreSQLDB status")
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
	}

	pgClient, err = r.PostgreSQLClients.GetClient(pgdb.GetKey())
	if err != nil {
		log.Error(err, "Failed to get PostgreSQL Client", "key", pgdb.GetKey())
		return ctrl.Result{}, err
	}

	if pgdb.Spec.SchemaMigrationFromGitHub == nil {
		return ctrl.Result{}, nil
	}

	driver, err := migratepostgres.WithInstance(pgClient, &migratepostgres.Config{DatabaseName: pgdb.Spec.DBName})
	if err != nil {
		log.Error(err, "failed to create migratepostgres.WithInstance")
		return ctrl.Result{}, err
	}
	m, err := migrate.NewWithDatabaseInstance(
		pgdb.Spec.SchemaMigrationFromGitHub.GetSourceUrl(),
		pgdb.Spec.DBName,
		driver,
	)
	if err != nil {
		log.Error(err, "failed to initialize NewWithDatabaseInstance")
		return ctrl.Result{}, err
	}
	err = m.Up()
	if err != nil {
		if err.Error() == "no change" {
			log.Info("migrate no change")
		} else {
			log.Error(err, "failed to Up")
			return ctrl.Result{}, err
		}
	}
	version, dirty, err := m.Version()
	if err != nil {
		return ctrl.Result{}, err
	}
	log.Info("migrate completed", "version", version, "dirty", dirty)
	pgdb.Status.SchemaMigration.Version = version
	pgdb.Status.SchemaMigration.Dirty = dirty
	if err := r.Status().Update(ctx, pgdb); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *PostgreSQLDBReconciler) finalizePostgreSQLDB(ctx context.Context, pgClient *sql.DB, pgdb *postgresv1alpha1.PostgreSQLDB) error {
	_, err := pgClient.ExecContext(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", pqQuoteIdentifier(pgdb.Spec.DBName)))
	return err
}

func (r *PostgreSQLDBReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&postgresv1alpha1.PostgreSQLDB{}).
		Complete(r)
}

func pqQuoteIdentifier(id string) string {
	return fmt.Sprintf("\"%s\"", id)
}
