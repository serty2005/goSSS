package app

import (
	"testing"

	"etalon-server/internal/infra/config"
	"etalon-server/internal/transport/http/handlers"

	"github.com/go-chi/chi/v5"
)

func TestBitrixModuleRegisterProtectedRoutes_RegistersContractSyncExecute(t *testing.T) {
	t.Parallel()

	module := &bitrixModule{
		cfg: &config.Config{
			EnableBitrixGateway: true,
		},
		bitrixHandler: handlers.NewBitrixHandler(nil, nil, nil),
	}

	router := chi.NewRouter()
	module.registerProtectedRoutes(router)

	routeContext := chi.NewRouteContext()
	matched := router.Match(routeContext, "POST", "/bitrix/service-points/contract-sync/execute")
	if !matched {
		t.Fatal("ожидался маршрут POST /bitrix/service-points/contract-sync/execute")
	}
}

func TestBitrixModuleRegisterProtectedRoutes_RegistersContractSyncRefresh(t *testing.T) {
	t.Parallel()

	module := &bitrixModule{
		cfg: &config.Config{
			EnableBitrixGateway: true,
		},
		bitrixHandler: handlers.NewBitrixHandler(nil, nil, nil),
	}

	router := chi.NewRouter()
	module.registerProtectedRoutes(router)

	routeContext := chi.NewRouteContext()
	matched := router.Match(routeContext, "POST", "/bitrix/service-points/contract-sync/refresh")
	if !matched {
		t.Fatal("ожидался маршрут POST /bitrix/service-points/contract-sync/refresh")
	}
}
