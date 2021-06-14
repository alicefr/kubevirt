package rbac

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	virtv1 "kubevirt.io/client-go/api/v1"
)

// Used for manifest generation only, not by the operator itself
func GetAllGuestfsImageConfigClusterRole() []interface{} {
	return []interface{}{
		NewGuestfsImageConfigClusterRole(),
		newGuestfsImageConfigClusterRoleBinding(),
	}
}

func NewGuestfsImageConfigClusterRole() *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRole",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "libguestfs.kubevirt.io:config-reader",
			Labels: map[string]string{
				virtv1.AppLabel: "",
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{
					"kubevirt.io",
				},
				Resources: []string{
					"gsconfigs",
				},
				Verbs: []string{
					"get", "list", "watch",
				},
			},
		},
	}
}

func newGuestfsImageConfigClusterRoleBinding() *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "libguestfs.kubevirt.io:config-reader",
			Labels: map[string]string{
				virtv1.AppLabel: "",
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "libguestfs.kubevirt.io:config-reader",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:     "Group",
				APIGroup: "rbac.authorization.k8s.io",
				Name:     "system:authenticated",
			},
			{
				Kind:     "Group",
				APIGroup: "rbac.authorization.k8s.io",
				Name:     "system:serviceaccount",
			},
		},
	}
}
