package controllers

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	postgresv1alpha1 "github.com/nakamasato/mysql-operator/api/postgresql/v1alpha1"
	pginternal "github.com/nakamasato/mysql-operator/internal/postgres"
	"github.com/nakamasato/mysql-operator/internal/secret"
	"github.com/nakamasato/mysql-operator/internal/utils"
)

const (
	postgresqlUserFinalizer                          = "postgresqluser.nakamasato.com/finalizer"
	postgresqlUserReasonCompleted                    = "Both secret and postgresql user are successfully created."
	postgresqlUserReasonPostgreSQLConnectionFailed   = "Failed to connect to postgresql"
	postgresqlUserReasonPostgreSQLFailedToCreateUser = "Failed to create PostgreSQL user"
	postgresqlUserReasonPostgreSQLFetchFailed        = "Failed to fetch PostgreSQL"
	postgresqlUserPhaseReady                         = "Ready"
	postgresqlUserPhaseNotReady                      = "NotReady"
)

// PostgreSQLUserReconciler reconciles a PostgreSQLUser object
type PostgreSQLUserReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	PostgreSQLClients pginternal.PostgreSQLClients
	SecretManagers    map[string]secret.SecretManager
}

//+kubebuilder:rbac:groups=postgres.nakamasato.com,resources=postgresqlusers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=postgres.nakamasato.com,resources=postgresqlusers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=postgres.nakamasato.com,resources=postgresqlusers/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create

func (r *PostgreSQLUserReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithName("PostgreSQLUserReconciler")
	pgUser := &postgresv1alpha1.PostgreSQLUser{}
	if err := r.Get(ctx, req.NamespacedName, pgUser); err != nil {
		if errors.IsNotFound(err) {
			log.Info("PostgreSQLUser not found", "req.NamespacedName", req.NamespacedName)
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get PostgreSQLUser")
		return ctrl.Result{}, err
	}

	pg := &postgresv1alpha1.PostgreSQL{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: pgUser.Spec.PostgresqlName}, pg); err != nil {
		log.Error(err, "Failed to fetch PostgreSQL")
		pgUser.Status.Phase = postgresqlUserPhaseNotReady
		pgUser.Status.Reason = postgresqlUserReasonPostgreSQLFetchFailed
		if serr := r.Status().Update(ctx, pgUser); serr != nil {
			log.Error(serr, "Failed to update PostgreSQLUser status")
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !r.ifOwnerReferencesContains(pgUser.OwnerReferences, pg) {
		if err := controllerutil.SetControllerReference(pg, pgUser, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Update(ctx, pgUser); err != nil {
			return ctrl.Result{}, err
		}
	}

	pgClient, err := r.PostgreSQLClients.GetClient(pg.GetKey())
	if err != nil {
		log.Error(err, "Failed to get PostgreSQL client", "key", pg.GetKey())
		return ctrl.Result{}, err
	}

	if !pgUser.GetDeletionTimestamp().IsZero() {
		if controllerutil.ContainsFinalizer(pgUser, postgresqlUserFinalizer) {
			if err := r.finalizePostgreSQLUser(ctx, pgClient, pgUser); err != nil {
				return ctrl.Result{}, err
			}
			if controllerutil.RemoveFinalizer(pgUser, postgresqlUserFinalizer) {
				if err := r.Update(ctx, pgUser); err != nil {
					return ctrl.Result{}, err
				}
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, nil
	}

	if controllerutil.AddFinalizer(pgUser, postgresqlUserFinalizer) {
		if err := r.Update(ctx, pgUser); err != nil {
			return ctrl.Result{}, err
		}
	}

	secretName := getPGSecretName(pg.Name, pgUser.Name)
	secretObj := &v1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: secretName}, secretObj); err == nil {
		return ctrl.Result{}, nil
	}

	password := utils.GenerateRandomString(16)
	_, err = pgClient.ExecContext(ctx, fmt.Sprintf("CREATE USER IF NOT EXISTS \"%s\" WITH PASSWORD '%s'", pgUser.Name, password))
	if err != nil {
		pgUser.Status.Phase = postgresqlUserPhaseNotReady
		pgUser.Status.Reason = postgresqlUserReasonPostgreSQLFailedToCreateUser
		if serr := r.Status().Update(ctx, pgUser); serr != nil {
			log.Error(serr, "Failed to update PostgreSQLUser status")
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	if err := r.createSecret(ctx, password, secretName, pgUser.Namespace, pgUser); err != nil {
		log.Error(err, "Failed to create secret")
		pgUser.Status.Reason = "Failed to create Secret"
		pgUser.Status.SecretCreated = false
		if serr := r.Status().Update(ctx, pgUser); serr != nil {
			log.Error(serr, "Failed to update PostgreSQLUser status")
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
		return ctrl.Result{}, err
	}
	pgUser.Status.Phase = postgresqlUserPhaseReady
	pgUser.Status.Reason = postgresqlUserReasonCompleted
	pgUser.Status.SecretCreated = true
	pgUser.Status.PostgreSQLUserCreated = true
	if err := r.Status().Update(ctx, pgUser); err != nil {
		log.Error(err, "Failed to update PostgreSQLUser status")
	}
	return ctrl.Result{}, nil
}

func (r *PostgreSQLUserReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&postgresv1alpha1.PostgreSQLUser{}).
		Complete(r)
}

func (r *PostgreSQLUserReconciler) finalizePostgreSQLUser(ctx context.Context, pgClient *sql.DB, pgUser *postgresv1alpha1.PostgreSQLUser) error {
	if pgUser.Status.PostgreSQLUserCreated {
		_, err := pgClient.ExecContext(ctx, fmt.Sprintf("DROP USER IF EXISTS \"%s\"", pgUser.Name))
		if err != nil {
			return err
		}
	}
	return nil
}

func getPGSecretName(pgName, pgUserName string) string {
	parts := []string{"postgresql", pgName, pgUserName}
	return strings.Join(parts, "-")
}

func (r *PostgreSQLUserReconciler) createSecret(ctx context.Context, password, secretName, namespace string, pgUser *postgresv1alpha1.PostgreSQLUser) error {
	data := map[string][]byte{"password": []byte(password)}
	secretObj := &v1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace}}
	if err := ctrl.SetControllerReference(pgUser, secretObj, r.Scheme); err != nil {
		return err
	}
	_, err := ctrl.CreateOrUpdate(ctx, r.Client, secretObj, func() error {
		secretObj.Data = data
		return nil
	})
	return err
}

func (r *PostgreSQLUserReconciler) ifOwnerReferencesContains(ownerReferences []metav1.OwnerReference, pg *postgresv1alpha1.PostgreSQL) bool {
	for _, ref := range ownerReferences {
		if ref.APIVersion == "postgres.nakamasato.com/v1alpha1" && ref.Kind == "PostgreSQL" && ref.UID == pg.UID {
			return true
		}
	}
	return false
}
