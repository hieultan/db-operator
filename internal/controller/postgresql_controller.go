package controllers

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	postgresv1alpha1 "github.com/nakamasato/mysql-operator/api/postgresql/v1alpha1"
	pginternal "github.com/nakamasato/mysql-operator/internal/postgres"
	"github.com/nakamasato/mysql-operator/internal/secret"
)

const postgresqlFinalizer = "postgresql.nakamasato.com/finalizer"

// PostgreSQLReconciler reconciles a PostgreSQL object
type PostgreSQLReconciler struct {
	client.Client
	Scheme               *runtime.Scheme
	PostgreSQLClients    pginternal.PostgreSQLClients
	PostgreSQLDriverName string
	SecretManagers       map[string]secret.SecretManager
}

//+kubebuilder:rbac:groups=postgres.nakamasato.com,resources=postgresqls,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=postgres.nakamasato.com,resources=postgresqls/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=postgres.nakamasato.com,resources=postgresqls/finalizers,verbs=update
//+kubebuilder:rbac:groups=postgres.nakamasato.com,resources=postgresqlusers,verbs=list
//+kubebuilder:rbac:groups=postgres.nakamasato.com,resources=postgresqldbs,verbs=list
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *PostgreSQLReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithName("PostgreSQLReconciler")
	pg := &postgresv1alpha1.PostgreSQL{}
	if err := r.Get(ctx, req.NamespacedName, pg); err != nil {
		if errors.IsNotFound(err) {
			log.Info("PostgreSQL not found", "name", req.NamespacedName)
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get PostgreSQL")
		return ctrl.Result{}, err
	}

	if controllerutil.AddFinalizer(pg, postgresqlFinalizer) {
		if err := r.Update(ctx, pg); err != nil {
			return ctrl.Result{}, err
		}
	}

	referencedUserNum, err := r.countReferencesByPostgreSQLUser(ctx, pg)
	if err != nil {
		return ctrl.Result{}, err
	}
	referencedDbNum, err := r.countReferencesByPostgreSQLDB(ctx, pg)
	if err != nil {
		return ctrl.Result{}, err
	}
	if pg.Status.UserCount != int32(referencedUserNum) || pg.Status.DBCount != int32(referencedDbNum) {
		pg.Status.UserCount = int32(referencedUserNum)
		pg.Status.DBCount = int32(referencedDbNum)
		if err := r.Status().Update(ctx, pg); err != nil {
			log.Error(err, "failed to update status (UserCount and DBCount)")
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
	}

	retry, err := r.UpdatePostgreSQLClients(ctx, pg)
	if err != nil {
		pg.Status.Connected = false
		pg.Status.Reason = err.Error()
		if serr := r.Status().Update(ctx, pg); serr != nil {
			log.Error(serr, "failed to update status")
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
		return ctrl.Result{}, err
	} else if retry {
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	connected, reason := true, "Ping succeeded and updated PostgreSQLClients"
	if pg.Status.Connected != connected || pg.Status.Reason != reason {
		pg.Status.Connected = connected
		pg.Status.Reason = reason
		if err := r.Status().Update(ctx, pg); err != nil {
			log.Error(err, "failed to update status")
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
	}

	if !pg.GetDeletionTimestamp().IsZero() && controllerutil.ContainsFinalizer(pg, postgresqlFinalizer) {
		if r.finalizePostgreSQL(ctx, pg) {
			if controllerutil.RemoveFinalizer(pg, postgresqlFinalizer) {
				if err := r.Update(ctx, pg); err != nil {
					return ctrl.Result{}, err
				}
			}
		} else {
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
	}
	return ctrl.Result{}, nil
}

func (r *PostgreSQLReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&postgresv1alpha1.PostgreSQL{}).
		Owns(&postgresv1alpha1.PostgreSQLUser{}).
		Owns(&postgresv1alpha1.PostgreSQLDB{}).
		Complete(r)
}

func (r *PostgreSQLReconciler) UpdatePostgreSQLClients(ctx context.Context, pg *postgresv1alpha1.PostgreSQL) (bool, error) {
	log := log.FromContext(ctx).WithName("PostgreSQLReconciler")
	user, password, err := r.getPostgreSQLConfig(ctx, pg)
	if err != nil {
		return true, err
	}
	if db, _ := r.PostgreSQLClients.GetClient(pg.GetKey()); db == nil {
		db, err := sql.Open(r.PostgreSQLDriverName, fmt.Sprintf("host=%s port=%d user=%s password=%s sslmode=disable", pg.Spec.Host, pg.Spec.Port, user, password))
		if err != nil {
			log.Error(err, "Failed to open PostgreSQL database", "postgresql.Name", pg.Name)
			return true, err
		}
		err = db.PingContext(ctx)
		if err != nil {
			log.Error(err, "Ping failed", "postgresql.Name", pg.Name)
			return true, err
		}
		r.PostgreSQLClients[pg.GetKey()] = db
		log.Info("Successfully added PostgreSQL client", "postgresql.Name", pg.Name)
	}

	pgDBList := &postgresv1alpha1.PostgreSQLDBList{}
	err = r.List(ctx, pgDBList, client.MatchingFields{"spec.postgresqlName": pg.Name})
	if err != nil {
		return true, err
	}
	for _, pgDB := range pgDBList.Items {
		if pgDB.Status.Phase != "Ready" {
			log.Info("postgresqlDB is not ready", "postgresqlDB", pgDB.Name)
			return true, nil
		}
		if _, err := r.PostgreSQLClients.GetClient(pgDB.GetKey()); err != nil {
			db, err := sql.Open(r.PostgreSQLDriverName, fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable", pg.Spec.Host, pg.Spec.Port, user, password, pgDB.Spec.DBName))
			if err != nil {
				return true, err
			}
			err = db.PingContext(ctx)
			if err != nil {
				return true, err
			}
			r.PostgreSQLClients[pgDB.GetKey()] = db
			log.Info("Successfully added PostgreSQL client", "postgresqldb.Name", pgDB.Name)
		}
	}

	return false, nil
}

func (r *PostgreSQLReconciler) getPostgreSQLConfig(ctx context.Context, pg *postgresv1alpha1.PostgreSQL) (user, password string, err error) {
	secretManager, ok := r.SecretManagers[pg.Spec.AdminPassword.Type]
	if !ok {
		return "", "", fmt.Errorf("the specified SecretManager type (%s) doesn't exist", pg.Spec.AdminPassword.Type)
	}
	password, err = secretManager.GetSecret(ctx, pg.Spec.AdminPassword.Name)
	if err != nil {
		return "", "", err
	}
	secretManager, ok = r.SecretManagers[pg.Spec.AdminUser.Type]
	if !ok {
		return "", "", fmt.Errorf("the specified SecretManager type (%s) doesn't exist", pg.Spec.AdminUser.Type)
	}
	user, err = secretManager.GetSecret(ctx, pg.Spec.AdminUser.Name)
	if err != nil {
		return "", "", err
	}
	return user, password, nil
}

func (r *PostgreSQLReconciler) countReferencesByPostgreSQLUser(ctx context.Context, pg *postgresv1alpha1.PostgreSQL) (int, error) {
	list := &postgresv1alpha1.PostgreSQLUserList{}
	err := r.List(ctx, list, client.MatchingFields{"spec.postgresqlName": pg.Name})
	if err != nil {
		return 0, err
	}
	return len(list.Items), nil
}

func (r *PostgreSQLReconciler) countReferencesByPostgreSQLDB(ctx context.Context, pg *postgresv1alpha1.PostgreSQL) (int, error) {
	list := &postgresv1alpha1.PostgreSQLDBList{}
	err := r.List(ctx, list, client.MatchingFields{"spec.postgresqlName": pg.Name})
	if err != nil {
		return 0, err
	}
	return len(list.Items), nil
}

func (r *PostgreSQLReconciler) finalizePostgreSQL(ctx context.Context, pg *postgresv1alpha1.PostgreSQL) bool {
	log := log.FromContext(ctx)
	if pg.Status.UserCount > 0 || pg.Status.DBCount > 0 {
		log.Info("still referenced", "UserCount", pg.Status.UserCount, "DBCount", pg.Status.DBCount)
		return false
	}
	if db, ok := r.PostgreSQLClients[pg.GetKey()]; ok {
		if err := db.Close(); err != nil {
			return false
		}
		delete(r.PostgreSQLClients, pg.GetKey())
		log.Info("Closed and removed PostgreSQL client", "postgresql.Key", pg.GetKey())
	}
	return true
}
