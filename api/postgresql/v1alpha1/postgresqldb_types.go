/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PostgreSQLDBSpec defines the desired state of PostgreSQLDB
type PostgreSQLDBSpec struct {

        // PostgreSQL (CRD) name to reference to, which decides the destination PostgreSQL server
        PostgresqlName string `json:"postgresqlName"`

        // PostgreSQL Database name
        DBName string `json:"dbName"`

        // PostgreSQL Database Schema Migrations from GitHub
        SchemaMigrationFromGitHub *GitHubConfig `json:"schemaMigrationFromGitHub,omitempty"`
}

// PostgreSQLDBStatus defines the observed state of PostgreSQLDB
type PostgreSQLDBStatus struct {
	// The phase of database creation
	Phase string `json:"phase,omitempty"`

	// The reason for the current phase
	Reason string `json:"reason,omitempty"`

	// Schema Migration status
	SchemaMigration SchemaMigration `json:"schemaMigration,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="The phase of PostgreSQLDB"
//+kubebuilder:printcolumn:name="Reason",type="string",JSONPath=".status.reason",description="The reason for the current phase of this PostgreSQLDB"
//+kubebuilder:printcolumn:name="SchemaMigration",type="string",JSONPath=".status.schemaMigration",description="schema_migration table if schema migration is enabled."

// PostgreSQLDB is the Schema for the postgresqldbs API
type PostgreSQLDB struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

        Spec   PostgreSQLDBSpec   `json:"spec,omitempty"`
        Status PostgreSQLDBStatus `json:"status,omitempty"`
}

func (m PostgreSQLDB) GetKey() string {
        return fmt.Sprintf("%s-%s-%s", m.Namespace, m.Spec.PostgresqlName, m.Spec.DBName)
}

//+kubebuilder:object:root=true

// PostgreSQLDBList contains a list of PostgreSQLDB
type PostgreSQLDBList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
        Items           []PostgreSQLDB `json:"items"`
}

// GitHubConfig holds GitHub repo, path, and ref for Data Migration
// https://github.com/golang-migrate/migrate/tree/master/source/github
type GitHubConfig struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	Path  string `json:"path"`
	Ref   string `json:"ref,omitempty"`
}

func (c GitHubConfig) GetSourceUrl() string {
	baseUrl := fmt.Sprintf("github://%s/%s/%s", c.Owner, c.Repo, c.Path)
	if c.Ref == "" {
		return baseUrl

	}
	return fmt.Sprintf("%s#%s", baseUrl, c.Ref)
}

// This reflect the schema_migration table
type SchemaMigration struct {
	Version uint `json:"version"`
	Dirty   bool `json:"dirty"`
}

func init() {
        SchemeBuilder.Register(&PostgreSQLDB{}, &PostgreSQLDBList{})
}
