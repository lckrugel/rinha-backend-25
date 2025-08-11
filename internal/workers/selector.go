package workers

import (
	"sync/atomic"

	"github.com/lckrugel/rinha-backend-25/internal/dtos"
)

type ServiceSelector struct {
	active atomic.Value // stores dtos.PaymentAPI
}

func NewServiceSelector() *ServiceSelector {
	s := &ServiceSelector{}
	s.active.Store(dtos.DEFAULT_API)
	return s
}

func (s *ServiceSelector) GetActive() dtos.PaymentAPI {
	v := s.active.Load()
	if v == nil {
		return dtos.DEFAULT_API
	}
	return v.(dtos.PaymentAPI)
}

func (s *ServiceSelector) SetActive(api dtos.PaymentAPI) {
	s.active.Store(api)
}
