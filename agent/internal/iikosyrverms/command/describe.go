package command

import "etalon-agent/internal/iikosyrverms/contract"

func HandleDescribe(version string) contract.DescribeResponse {
	return contract.NewDescribeResponse(version)
}
