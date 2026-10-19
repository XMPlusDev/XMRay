package api

type API interface {
	GetNodeInfo() (nodeInfo *NodeInfo, err error)
	GetTransitNode() (nodeInfo *RelayNodeInfo, err error)
	GetSubscriptionList() (subscriptionList *[]SubscriptionInfo, err error)
	ReportOnlineIPs(onlineIP *[]OnlineIP) (err error)
	ReportTraffic(subscriptionTraffic *[]SubscriptionTraffic) (err error)
	ReportServerStatus(status *ServerStatus) (err error)
	ReportCertHash(data *CertHash) (err error)
	GetServerNodes() (*ServerNodesResponse, error)
	Describe() ClientInfo
	Debug()
}
