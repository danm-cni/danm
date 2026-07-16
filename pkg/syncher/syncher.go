package syncher

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	current "github.com/containernetworking/cni/pkg/types/040"
)

const (
	MaximumAllowedTime = 3000 // Timeout = MaximumAllowedTime * RetryInterval[ms] = 30s
	RetryInterval      = 10   // [ms]
	DefaultIfName      = "eth0"
)

type cniOpResult struct {
	CniName   string
	OpResult  error
	IfName    string
	CniResult *current.Result
}

type Syncher struct {
	ExpectedNumOfResults int
	CniResults           []cniOpResult
	mux                  sync.Mutex
}

func NewSyncher(numOfResults int) *Syncher {
	syncher := Syncher{}
	syncher.ExpectedNumOfResults = numOfResults
	return &syncher
}

func (synch *Syncher) PushResult(cniName string, opRes error, cniRes *current.Result, ifName string) {
	synch.mux.Lock()
	defer synch.mux.Unlock()
	cniOpResult := cniOpResult{
		CniName:   cniName,
		OpResult:  opRes,
		CniResult: cniRes,
		IfName:    ifName,
	}
	synch.CniResults = append(synch.CniResults, cniOpResult)
}

func (synch *Syncher) GetAggregatedResult() error {
	//Time-out Pod creation if plugins did not provide results within the configured timeframe
	for i := 0; i < MaximumAllowedTime; i++ {
		if synch.ExpectedNumOfResults > len(synch.CniResults) {
			time.Sleep(RetryInterval * time.Millisecond)
			continue
		}
		if synch.wasAnyOperationErroneous() {
			return synch.mergeErrorMessages()
		}
		return nil
	}
	return errors.New("CNI operation timed-out after " + strconv.Itoa(MaximumAllowedTime*RetryInterval/1000) + " seconds")
}

func (synch *Syncher) wasAnyOperationErroneous() bool {
	for _, cniRes := range synch.CniResults {
		if cniRes.OpResult != nil {
			return true
		}
	}
	return false
}

func (synch *Syncher) mergeErrorMessages() error {
	var aggregatedErrors []string
	for _, cniRes := range synch.CniResults {
		if cniRes.OpResult != nil {
			aggregatedErrors = append(aggregatedErrors, "CNI operation for network:"+cniRes.CniName+" failed with:"+cniRes.OpResult.Error())
		}
	}
	return fmt.Errorf(strings.Join(aggregatedErrors, "\n"))
}

func (synch *Syncher) MergeCniResults() *current.Result {
	aggregatedCniRes := current.Result{}
	//Since nobody follows CNI specification we need take care of "sorting" the CNI result properly
	//The frist IP(s) in the list will be chosen as the PodIP by the CRI (at least containerd seems to behave this way),
	//so we must make sure IPs belonging to the cluster network, aka. eth0 interface are put in the front
	for _, cniRes := range synch.CniResults {
		if cniRes.CniResult != nil && cniRes.IfName == DefaultIfName {
			aggregatedCniRes.Interfaces = append(aggregatedCniRes.Interfaces, cniRes.CniResult.Interfaces...)
			aggregatedCniRes.IPs = append(aggregatedCniRes.IPs, cniRes.CniResult.IPs...)
			aggregatedCniRes.Routes = append(aggregatedCniRes.Routes, cniRes.CniResult.Routes...)
		}
	}
	for _, cniRes := range synch.CniResults {
		//And here comes the rest
		if cniRes.CniResult != nil && cniRes.IfName != DefaultIfName {
			aggregatedCniRes.Interfaces = append(aggregatedCniRes.Interfaces, cniRes.CniResult.Interfaces...)
			aggregatedCniRes.IPs = append(aggregatedCniRes.IPs, cniRes.CniResult.IPs...)
			aggregatedCniRes.Routes = append(aggregatedCniRes.Routes, cniRes.CniResult.Routes...)
		}
	}
	return &aggregatedCniRes
}

func (synch *Syncher) WasAnyOperationErroneous() bool {
	if len(synch.CniResults) == 0 {
		return false
	}
	return synch.wasAnyOperationErroneous()
}
