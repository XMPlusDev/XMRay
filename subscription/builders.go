package subscription

import (
	"log"
	"strings"

	"github.com/xmplusdev/xmray/api"

	C "github.com/sagernet/sing/common"
	"github.com/sagernet/sing-shadowsocks/shadowaead_2022"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/infra/conf"
	"github.com/xtls/xray-core/proxy/masque"
	hysteria "github.com/xtls/xray-core/proxy/hysteria/account"
	"github.com/xtls/xray-core/proxy/shadowsocks"
	"github.com/xtls/xray-core/proxy/shadowsocks_2022"
	"github.com/xtls/xray-core/proxy/trojan"
	"github.com/xtls/xray-core/proxy/vless"
)

func BuildVmessUsers(subscriptionInfo *[]api.SubscriptionInfo, tag string) []*protocol.User {
	users := make([]*protocol.User, 0, len(*subscriptionInfo))
	for _, subscription := range *subscriptionInfo {
		vmessAccount := &conf.VMessAccount{
			ID:       subscription.UUID,
			Security: "auto",
		}
		users = append(users, &protocol.User{
			Level:   0,
			Email:   buildUserTag(tag, &subscription),
			Account: serial.ToTypedMessage(vmessAccount.Build()),
		})
	}
	return users
}

func BuildVlessUsers(subscriptionInfo *[]api.SubscriptionInfo, flow string, tag string) []*protocol.User {
	users := make([]*protocol.User, 0, len(*subscriptionInfo))
	for _, subscription := range *subscriptionInfo {
		users = append(users, &protocol.User{
			Level: 0,
			Email: buildUserTag(tag, &subscription),
			Account: serial.ToTypedMessage(&vless.Account{
				Id:   subscription.UUID,
				Flow: flow,
			}),
		})
	}
	return users
}

func BuildTrojanUsers(subscriptionInfo *[]api.SubscriptionInfo, tag string) []*protocol.User {
	users := make([]*protocol.User, 0, len(*subscriptionInfo))
	for _, subscription := range *subscriptionInfo {
		users = append(users, &protocol.User{
			Level: 0,
			Email: buildUserTag(tag, &subscription),
			Account: serial.ToTypedMessage(&trojan.Account{
				Password: subscription.UUID,
			}),
		})
	}
	return users
}

func BuildShadowsocksUsers(subscriptionInfo *[]api.SubscriptionInfo, method string, tag string) []*protocol.User {
	cypherMethod := "aes-128-gcm"
	if method != "" {
		cypherMethod = method
	}

	users := make([]*protocol.User, 0, len(*subscriptionInfo))
	for _, subscription := range *subscriptionInfo {
		if C.Contains(shadowaead_2022.List, strings.ToLower(cypherMethod)) {
			users = append(users, &protocol.User{
				Level: 0,
				Email: buildUserTag(tag, &subscription),
				Account: serial.ToTypedMessage(&shadowsocks_2022.Account{
					Key: subscription.Passwd,
				}),
			})
		} else {
			users = append(users, &protocol.User{
				Level: 0,
				Email: buildUserTag(tag, &subscription),
				Account: serial.ToTypedMessage(&shadowsocks.Account{
					Password:   subscription.Passwd,
					CipherType: getCipherType(method),
				}),
			})
		}
	}
	return users
}

func BuildHysteriaUsers(subscriptionInfo *[]api.SubscriptionInfo, tag string) []*protocol.User {
	users := make([]*protocol.User, 0, len(*subscriptionInfo))
	for _, subscription := range *subscriptionInfo {
		users = append(users, &protocol.User{
			Level: 0,
			Email: buildUserTag(tag, &subscription),
			Account: serial.ToTypedMessage(&hysteria.Account{
				Auth: subscription.UUID,
			}),
		})
	}
	return users
}

func BuildMasqueUsers(subscriptionInfo *[]api.SubscriptionInfo, tag string) []*protocol.User {
	users := make([]*protocol.User, 0, len(*subscriptionInfo))
	for _, subscription := range *subscriptionInfo {
		users = append(users, &protocol.User{
			Level: 0,
			Email: buildUserTag(tag, &subscription),
			Account: serial.ToTypedMessage(&masque.Account{
				Password: subscription.UUID,
			}),
		})
	}
	return users
}

func getCipherType(method string) shadowsocks.CipherType {
	switch strings.ToLower(method) {
	case "aes-128-gcm":
		return shadowsocks.CipherType_AES_128_GCM
	case "aes-256-gcm":
		return shadowsocks.CipherType_AES_256_GCM
	case "chacha20-poly1305", "chacha20-ietf-poly1305":
		return shadowsocks.CipherType_CHACHA20_POLY1305
	default:
		log.Printf("Warning: unknown cipher method %s, defaulting to AES_256_GCM", method)
		return shadowsocks.CipherType_AES_256_GCM
	}
}
