// Code generated by swagger-doc. DO NOT EDIT.

package v1alpha1

func (StorageMigration) SwaggerDoc() map[string]string {
	return map[string]string{
		"":       "StorageMigration defines the operation of moving the storage to another\nstorage backend\n+genclient\n+k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object",
		"status": "+optional",
	}
}

func (StorageMigrationList) SwaggerDoc() map[string]string {
	return map[string]string{
		"":      "StorageMigrationList is a list of StorageMigration resources\n+k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object",
		"items": "+listType=atomic",
	}
}

func (MigratedVolume) SwaggerDoc() map[string]string {
	return map[string]string{}
}

func (MigrationStorageClass) SwaggerDoc() map[string]string {
	return map[string]string{}
}

func (StorageMigrationSpec) SwaggerDoc() map[string]string {
	return map[string]string{
		"":                       "StorageMigrationSpec is the spec for a StorageMigration resource",
		"migratedVolume":         "MigratedVolumes is a list of volumes to be migrated\n+optional",
		"migrationStorageClass":  "MigrationStorageClass contains the information for relocating the\nvolumes of the source storage class to the destination storage class\n+optional",
		"reclaimPolicySourcePvc": "ReclaimPolicySourcePvc describes how the source volumes will be\ntreated after a successful migration\n+optional",
	}
}

func (StorageMigrationStatus) SwaggerDoc() map[string]string {
	return map[string]string{
		"":               "StorageMigrationStatus is the status for a StorageMigration resource",
		"migratedVolume": "MigratedVolumes is a list of volumes to be migrated\n+optional",
	}
}
