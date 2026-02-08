// To implement an api , one needs to implement the interface below.

package api

type API interface {
	GetNodeInfo() (nodeInfo *NodeInfo, err error)
	GetTransitNode() (nodeInfo *RelayNodeInfo, err error)
	GetSubscriptionList() (subscriptionList *[]SubscriptionInfo, err error)
	ReportOnlineIPs(onlineIP *[]OnlineIP) (err error)
	ReportTraffic(subscriptionTraffic *[]SubscriptionTraffic) (err error)
	Describe() ClientInfo
	Debug()
}
