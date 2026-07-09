//go:generate bash -c "cg(){ go list -m -f {{.Dir}} k8s.io/code-generator;}; crd(){ cat<<<'github.com/danm-cni/danm/crd';}; GOFLAGS='' bash $(cg)/generate-groups.sh all $(crd)/client/nad $(crd)/apis k8s.cni.cncf.io:v1 --go-header-file $(cg)/hack/boilerplate.go.txt"
// +k8s:deepcopy-gen=package
// +groupName=k8s.cni.cncf.io
// +groupGoName=K8sCniCncfIo

package v1
