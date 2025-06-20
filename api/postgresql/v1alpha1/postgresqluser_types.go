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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PostgreSQLUserSpec defines the desired state of PostgreSQLUser
type PostgreSQLUserSpec struct {

        // PostgreSQL (CRD) name to reference to, which decides the destination PostgreSQL server
        PostgresqlName string `json:"postgresqlName"`

	// +kubebuilder:default=%
	// +kubebuilder:validation:Optional

        // PostgreSQL hostname for the account
        Host string `json:"host"`
}

// PostgreSQLUserStatus defines the observed state of PostgreSQLUser
type PostgreSQLUserStatus struct {

	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
	Phase      string             `json:"phase,omitempty"`
	Reason     string             `json:"reason,omitempty"`

	// +kubebuilder:default=false

        // true if PostgreSQL user is created
        PostgreSQLUserCreated bool `json:"postgresql_user_created,omitempty"`

	// +kubebuilder:default=false

	// true if Secret is created
	SecretCreated bool `json:"secret_created,omitempty"`
}

func (m *PostgreSQLUser) GetConditions() []metav1.Condition {
        return m.Status.Conditions
}

func (m *PostgreSQLUser) SetConditions(conditions []metav1.Condition) {
        m.Status.Conditions = conditions
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="PostgreSQLUser",type="boolean",JSONPath=".status.postgresql_user_created",description="true if PostgreSQL user is created"
//+kubebuilder:printcolumn:name="Secret",type="boolean",JSONPath=".status.secret_created",description="true if Secret is created"
//+kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="The phase of this PostgreSQLUser"
//+kubebuilder:printcolumn:name="Reason",type="string",JSONPath=".status.reason",description="The reason for the current phase of this PostgreSQLUser"

// PostgreSQLUser is the Schema for the postgresqlusers API
type PostgreSQLUser struct {
        metav1.TypeMeta   `json:",inline"`
        metav1.ObjectMeta `json:"metadata,omitempty"`

        Spec   PostgreSQLUserSpec   `json:"spec,omitempty"`
        Status PostgreSQLUserStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// PostgreSQLUserList contains a list of PostgreSQLUser
type PostgreSQLUserList struct {
        metav1.TypeMeta `json:",inline"`
        metav1.ListMeta `json:"metadata,omitempty"`
        Items           []PostgreSQLUser `json:"items"`
}

func init() {
        SchemeBuilder.Register(&PostgreSQLUser{}, &PostgreSQLUserList{})
}
