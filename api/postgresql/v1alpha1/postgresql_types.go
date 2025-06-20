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

// PostgreSQLSpec holds the connection information for the target PostgreSQL server.
type PostgreSQLSpec struct {

        // Host is PostgreSQL server host.
        Host string `json:"host"`

        //+kubebuilder:default=5432

        // Port is PostgreSQL server port.
        Port int16 `json:"port,omitempty"`

        // AdminUser is PostgreSQL user to connect target server.
        AdminUser Secret `json:"adminUser"`

        // AdminPassword is PostgreSQL password to connect target server.
        AdminPassword Secret `json:"adminPassword"`
}

// PostgreSQLStatus defines the observed state of PostgreSQL
type PostgreSQLStatus struct {
        // true if successfully connected to the PostgreSQL server
        Connected bool `json:"connected,omitempty"`

	// Reason for connection failure
	Reason string `json:"reason,omitempty"`

	//+kubebuilder:default=0

        // The number of users in this PostgreSQL
        UserCount int32 `json:"userCount"`

	//+kubebuilder:default=0

        // The number of database in this PostgreSQL
        DBCount int32 `json:"dbCount"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Host",type=string,JSONPath=`.spec.host`
//+kubebuilder:printcolumn:name="AdminUser",type=string,JSONPath=`.spec.adminUser.name`
//+kubebuilder:printcolumn:name="Connected",type=boolean,JSONPath=`.status.connected`
//+kubebuilder:printcolumn:name="UserCount",type="integer",JSONPath=".status.userCount",description="The number of PostgreSQLUsers that belongs to the PostgreSQL"
//+kubebuilder:printcolumn:name="DBCount",type="integer",JSONPath=".status.dbCount",description="The number of PostgreSQLDBs that belongs to the PostgreSQL"
//+kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.reason`

// PostgreSQL is the Schema for the postgreses API
type PostgreSQL struct {
        metav1.TypeMeta   `json:",inline"`
        metav1.ObjectMeta `json:"metadata,omitempty"`

        Spec   PostgreSQLSpec   `json:"spec,omitempty"`
        Status PostgreSQLStatus `json:"status,omitempty"`
}

func (m PostgreSQL) GetKey() string {
        return fmt.Sprintf("%s-%s", m.Namespace, m.Name)
}

//+kubebuilder:object:root=true

// PostgreSQLList contains a list of PostgreSQL
type PostgreSQLList struct {
        metav1.TypeMeta `json:",inline"`
        metav1.ListMeta `json:"metadata,omitempty"`
        Items           []PostgreSQL `json:"items"`
}

type Secret struct {
	// Secret Name
	Name string `json:"name"`

	// +kubebuilder:validation:Enum=raw;gcp;k8s

	// Secret Type (e.g. gcp, raw, k8s)
	Type string `json:"type"`
}

func init() {
        SchemeBuilder.Register(&PostgreSQL{}, &PostgreSQLList{})
}
