#!/bin/bash

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

version="v1.0.0"

arch=$(uname -m)
kernelArch=$arch
case $arch in
	"i386" | "i686")
		kernelArch=32
		;;
	"x86_64" | "amd64" | "x64")
		kernelArch=64
		;;
	"arm64" | "armv8l" | "aarch64")
		kernelArch="arm64-v8a"
		;;
esac

# check root
[[ $EUID -ne 0 ]] && echo -e "${red}Error: ${plain} This script must be run with the root user！\n" && exit 1

# check os
if [[ -f /etc/redhat-release ]]; then
    release="centos"
elif cat /etc/issue | grep -Eqi "debian"; then
    release="debian"
elif cat /etc/issue | grep -Eqi "ubuntu"; then
    release="ubuntu"
elif cat /etc/issue | grep -Eqi "centos|red hat|redhat"; then
    release="centos"
elif cat /proc/version | grep -Eqi "debian"; then
    release="debian"
elif cat /proc/version | grep -Eqi "ubuntu"; then
    release="ubuntu"
elif cat /proc/version | grep -Eqi "centos|red hat|redhat"; then
    release="centos"
else
    echo -e "${red}System version not detected, please contact the script author！${plain}\n" && exit 1
fi

os_version=""

# os version
if [[ -f /etc/os-release ]]; then
    os_version=$(awk -F'[= ."]' '/VERSION_ID/{print $3}' /etc/os-release)
fi
if [[ -z "$os_version" && -f /etc/lsb-release ]]; then
    os_version=$(awk -F'[= ."]+' '/DISTRIB_RELEASE/{print $2}' /etc/lsb-release)
fi

if [[ x"${release}" == x"centos" ]]; then
    if [[ ${os_version} -le 6 ]]; then
        echo -e "${red}Please use CentOS 7 or later!${plain}\n" && exit 1
    fi
elif [[ x"${release}" == x"ubuntu" ]]; then
    if [[ ${os_version} -lt 16 ]]; then
        echo -e "${red}Please use Ubuntu 16 or later system！${plain}\n" && exit 1
    fi
elif [[ x"${release}" == x"debian" ]]; then
    if [[ ${os_version} -lt 8 ]]; then
        echo -e "${red}Please use Debian 8 or higher！${plain}\n" && exit 1
    fi
fi
 
confirm() {
    if [[ $# > 1 ]]; then
        echo && read -p "$1 [Default$2]: " temp
        if [[ x"${temp}" == x"" ]]; then
            temp=$2
        fi
    else
        read -p "$1 [y/n]: " temp
    fi
    if [[ x"${temp}" == x"y" || x"${temp}" == x"Y" ]]; then
        return 0
    else
        return 1
    fi
}

confirm_restart() {
    confirm "Whether to restart XMRay " "y"
    if [[ $? == 0 ]]; then
        restart
    else
        show_menu
    fi
}

before_show_menu() {
    echo && echo -n -e "${yellow}Press enter to return to the main menu: ${plain} " && read temp
    show_menu
}

install() {
    bash <(curl -Ls https://raw.githubusercontent.com/XMPlusDev/XMRay/script/install.sh)
    if [[ $? == 0 ]]; then
        if [[ $# == 0 ]]; then
            start
        else
            start 0
        fi
    fi
}

update() {
    version=$2
    bash <(curl -Ls https://raw.githubusercontent.com/XMPlusDev/XMRay/script/install.sh) $version
}

config() {
    echo "XMRay will automatically try to restart after modifying the configuration"
    vi /etc/XMRay/config.yml
    sleep 2
    check_status
    case $? in
        0)
            echo -e "XMRay Status: ${green}Running${plain}"
            ;;
        1)
            echo -e "It is detected that you have not started XMRay or XMRay failed to restart automatically, check the log？[Y/n]" && echo
            read -e -p "(Default: y):" yn
            [[ -z ${yn} ]] && yn="y"
            if [[ ${yn} == [Yy] ]]; then
               show_log
            fi
            ;;
        2)
            echo -e "XMRay Status: ${red}Not Installed${plain}"
    esac
}

uninstall() {
    confirm "Are you sure you want to uninstall XMRay? " "n"
    if [[ $? != 0 ]]; then
        if [[ $# == 0 ]]; then
            show_menu
        fi
        return 0
    fi
	if [ -e "/etc/systemd/system/" ] ; then
		systemctl stop XMRay
		systemctl disable XMRay
		rm /etc/systemd/system/XMRay.service -f
		systemctl daemon-reload
		systemctl reset-failed
	else
		rc-service XMRay stop
		rc-update delete XMRay default 
		rc-update --update
		rm /etc/init.d/XMRay/XMRay.rc -f
		systemctl daemon-reload
	fi
	
    rm /etc/XMRay/ -rf
    rm /usr/local/XMRay/ -rf

    echo ""
    echo -e "The uninstallation is successful. If you want to delete this script, run ssh command ${green}rm -rf /usr/bin/XMRay -f ${plain} to delete"
    echo ""

    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

start() {
    check_status
    if [[ $? == 0 ]]; then
        echo ""
        echo -e "${green}XMRay aready running, no need to start again, if you need to restart, please select restart${plain}"
    else
		systemctl start XMRay
		sleep 2
		check_status
		if [[ $? == 0 ]]; then
			echo -e "${green}XMRay startup is successful, please use XMRay log to view the operation log${plain}"
		else
			echo -e "${red}XMRay may fail to start, please use XMRay log to check the log information later${plain}"
		fi
    fi

    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

stop() {
	systemctl stop XMRay
	sleep 2
	check_status
	if [[ $? == 1 ]]; then
		echo -e "${green}XMRay stop successful${plain}"
	else
		echo -e "${red}XMRay stop failed, probably because the stop time exceeded two seconds, please check the log information later${plain}"
	fi

    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

restart() {
	systemctl restart XMRay
	sleep 2
	check_status
	if [[ $? == 0 ]]; then
		echo -e "${green}XMRay restart is successful, please use XMRay log to view the operation log${plain}"
	else
		echo -e "${red}XMRay may fail to start, please use XMRay log to check the log information later${plain}"
	fi

    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

status() {
	systemctl status XMRay --no-pager -l
    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

enable() {
	systemctl enable XMRay
	if [[ $? == 0 ]]; then
		echo -e "${green}start XMRay on system boot successfully enabled${plain}"
	else
		echo -e "${red}start XMRay on system boot failed to enable${plain}"
	fi

    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

disable() {
	systemctl disable XMRay
	if [[ $? == 0 ]]; then
		echo -e "${green}diable XMRay on system boot successfull${plain}"
	else
		echo -e "${red}diable XMRay on system boot failed${plain}"
	fi
    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

show_log() {
    journalctl -u XMRay.service -e --no-pager -f
    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

install_bbr() {
    bash <(curl -L -s https://raw.githubusercontent.com/chiakge/Linux-NetSpeed/master/tcp.sh)
}

update_script() {
	systemctl stop XMRay
	rm -rf /usr/bin/XMRay
	rm -rf /usr/bin/XMRay
	systemctl daemon-reload
    wget -O /usr/bin/XMRay -N --no-check-certificate https://raw.githubusercontent.com/XMPlusDev/XMRay/script/XMRay.sh
    if [[ $? != 0 ]]; then
        echo ""
        echo -e "${red}Failed to download the script, please check whether the machine can connect Github${plain}"
        before_show_menu
    else
        chmod +x /usr/bin/XMRay
		ln -s /usr/bin/XMRay /usr/bin/xmray 
		chmod +x /usr/bin/XMRay
		systemctl start XMRay
        echo -e "${green}The upgrade script was successful, please run the script again${plain}" && exit 0
    fi
}

# 0: running, 1: not running, 2: not installed
check_status() {
		if [[ ! -f /etc/systemd/system/XMRay.service ]]; then
			return 2
		fi
	
		temp=$(systemctl status XMRay | grep Active | awk '{print $3}' | cut -d "(" -f2 | cut -d ")" -f1)
		if [[ x"${temp}" == x"running" ]]; then
			return 0
		else
			return 1
		fi

}

check_enabled() {
		temp=$(systemctl is-enabled XMRay)
		if [[ x"${temp}" == x"enabled" ]]; then
			return 0
		else
			return 1;
		fi
}

check_uninstall() {
    check_status
    if [[ $? != 2 ]]; then
        echo ""
        echo -e "${red}XMRay already installed, please do not repeat the installation${plain}"
        if [[ $# == 0 ]]; then
            before_show_menu
        fi
        return 1
    else
        return 0
    fi
}

check_install() {
    check_status
    if [[ $? == 2 ]]; then
        echo ""
        echo -e "${red}please install XMRay first${plain}"
        if [[ $# == 0 ]]; then
            before_show_menu
        fi
        return 1
    else
        return 0
    fi
}

show_status() {
    check_status
    case $? in
        0)
            echo -e "XMRay Status: ${green}Running${plain}"
            show_enable_status
            ;;
        1)
            echo -e "XMRay Status: ${yellow}Not Running${plain}"
            show_enable_status
            ;;
        2)
            echo -e "XMRay Status: ${red}Not Installed${plain}"
    esac
}

show_enable_status() {
    check_enabled
    if [[ $? == 0 ]]; then
        echo -e "Whether to start automatically: ${green}Yes${plain}"
    else
        echo -e "Whether to start automatically: ${red}No${plain}"
    fi
}

show_XMRay_version() {
    echo -n ""
    /usr/local/XMRay/XMRay version
    echo ""
    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

show_XMRay_warp() {
    echo -n ""
    /usr/local/XMRay/XMRay warp
    echo ""
    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

show_XMRay_x25519() {
     echo -n ""
    /usr/local/XMRay/XMRay x25519
    echo ""
    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

show_XMRay_mldsa65() {
     echo -n ""
    /usr/local/XMRay/XMRay mldsa65
    echo ""
    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

show_XMRay_mlkem768() {
     echo -n ""
    /usr/local/XMRay/XMRay mlkem768
    echo ""
    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

show_XMRay_vlessenc() {
     echo -n ""
    /usr/local/XMRay/XMRay vlessenc
    echo ""
    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

show_XMRay_ping() {
    echo -n ""
    read -p "Enter domain name (e.g., example.com or example.com:443): " domain
    
    echo "TLS pinging $domain..."
    /usr/local/XMRay/XMRay tls ping "$domain"
    
    echo ""
    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

show_XMRay_certObtain() {  
    echo -n ""
    read -p "Enter domain name: " domain
    read -p "Enter email address: " email
    
    echo "Obtaining certificate for $domain using HTTP challenge..."
    /usr/local/XMRay/XMRay cert obtain -d "$domain" -e "$email" 
    
    echo ""
    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

show_XMRay_certRenew() {
    echo -n ""
    read -p "Enter domain name: " domain
    read -p "Enter email address: " email
    
    echo "Renewing certificate for $domain using HTTP challenge..."
    /usr/local/XMRay/XMRay cert renew -d "$domain" -e "$email"
    
    echo ""
    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

show_XMRay_tls_generate() {
    echo -n ""
    read -p "Enter domain name (eg, tld.com): " domain
    
    /usr/local/XMRay/XMRay tls generate --domain "$domain" --file "/ect/XMRay/$domain"
    
    echo ""
    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

show_XMRay_tls_ech() {
    echo -n ""
    read -p "Enter serverName: " serverName
    
    /usr/local/XMRay/XMRay tls ech --serverName "$serverName"
    
    echo ""
    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

show_XMRay_tls_hash() {
    echo -n ""
    read -p "Enter cert file(.crt) path: " file
    
    /usr/local/XMRay/XMRay tls hash --cert "$file"
    
    echo ""
    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}
 

show_usage() {
    echo "XMRay management script: "
    echo "------------------------------------------"
    echo "XMRay                    - Show menu (more features)"
    echo "XMRay start              - Start XMRay"
    echo "XMRay stop               - Stop XMRay"
    echo "XMRay restart            - Restart XMRay"
    echo "XMRay status             - View XMRay status"
    echo "XMRay enable             - Enable XMRay auto-start"
    echo "XMRay disable            - Disable XMRay auto-start"
    echo "XMRay log                - View XMRay logs"
    echo "XMRay update             - Update XMRay"
    echo "XMRay update vx.x.x      - Update XMRay Specific version"
    echo "XMRay config             - Show configuration file content"
    echo "XMRay install            - Install XMRay"
    echo "XMRay uninstall          - Uninstall XMRay"
    echo "XMRay version            - View XMRay version"
    echo "XMRay warp               - Generate cloudflare warp account"
    echo "XMRay x25519             - Generate key pair for X25519 key exchange (REALITY, VLESS Encryption)"
    echo "XMRay mldsa65            - Generate key pair for ML-DSA-65 post-quantum signature (REALITY)"
    echo "XMRay mlkem768           - Generate key pair for ML-KEM-768 post-quantum key exchange (VLESS Encryption)"
    echo "XMRay vlessenc           - Generate decryption/encryption json pair (VLESS Encryption)" 
    echo "XMRay ping               - Ping a domain with TLS handshake" 
    echo "XMRay obtain             - Generate SSL/TLS certificate for domain name" 
    echo "XMRay renew              - Renew SSL/TLS certificate for domain name" 
    echo "XMRay ech                - Generate ECH keys with default or custom server name"
    echo "XMRay hash               - Calculate hash for specific certificate"
    echo "XMRay generate           - Generate self-signed TLS certificates for testing and production use"	
    echo "------------------------------------------"
}


show_menu() {
    echo -e "
  ${green}XMRay backend management script，${plain}${red}not applicable to docker${plain}
--- https://github.com/XMPlusDev/XMRay ---
  ${green}0.${plain} Change setting
————————————————
  ${green}1.${plain} Install XMRay
  ${green}2.${plain} Update XMRay
  ${green}3.${plain} Uninstall XMRay
————————————————
  ${green}4.${plain} start XMRay
  ${green}5.${plain} Stop XMRay
  ${green}6.${plain} Restart XMRay
  ${green}7.${plain} View XMRay Status
  ${green}8.${plain} View XMRay log
————————————————
  ${green}9.${plain} Enable XMRay auto-satrt
 ${green}10.${plain} Disable XMRay auto-satrt
————————————————
 ${green}11.${plain} One-click install bbr (latest kernel)
 ${green}12.${plain} View XMRay version 
 ${green}13.${plain} Upgrade maintenance script
————————————————
 ${green}14.${plain} Generate cloudflare warp account info
 ${green}15.${plain} Generate key pair for X25519 key exchange (REALITY, VLESS Encryption)
 ${green}16.${plain} Generate key pair for ML-DSA-65 post-quantum signature (REALITY)
 ${green}17.${plain} Generate key pair for ML-KEM-768 post-quantum key exchange (VLESS Encryption)
 ${green}18.${plain} Generate decryption/encryption json pair (VLESS Encryption)
 ${green}19.${plain} Ping the domain with TLS handshake
 ${green}20.${plain} Generate SSL/TLS certificate for domain name
 ${green}21.${plain} Renew SSL/TLS certificate for domain name
 
 ${green}22.${plain} Generate ECH keys with default or custom server name
 ${green}23.${plain} Calculate hash for specific certificate
 ${green}24.${plain} Generate self-signed TLS certificates for testing and production use
 "
    show_status
    echo && read -p "Please enter selection [0-13]: " num

    case "${num}" in
        0) config
        ;;
        1) check_uninstall && install
        ;;
        2) check_install && update
        ;;
        3) check_install && uninstall
        ;;
        4) check_install && start
        ;;
        5) check_install && stop
        ;;
        6) check_install && restart
        ;;
        7) check_install && status
        ;;
        8) check_install && show_log
        ;;
        9) check_install && enable
        ;;
        10) check_install && disable
        ;;
        11) install_bbr
        ;;
        12) check_install && show_XMRay_version
        ;;
        13) update_script
        ;;
		14) check_install && show_XMRay_warp
        ;;
		15) check_install && show_XMRay_x25519
        ;;
		16) check_install && show_XMRay_mldsa65
        ;;
		17) check_install && show_XMRay_mlkem768
        ;;
		18) check_install && show_XMRay_vlessenc
        ;;
		19) check_install && show_XMRay_ping
        ;;
		20) check_install && show_XMRay_certObtain
        ;;
		21) check_install && show_XMRay_certRenew
        ;;
		22) check_install && show_XMRay_tls_ech
        ;;
		23) check_install && show_XMRay_tls_hash
        ;;
		24) check_install && show_XMRay_tls_generate
        ;;
        *) echo -e "${red}Please enter the correct number [0-24]${plain}"
        ;;
    esac
}


if [[ $# > 0 ]]; then
    case $1 in
        "start") check_install 0 && start 0
        ;;
        "stop") check_install 0 && stop 0
        ;;
        "restart") check_install 0 && restart 0
        ;;
        "status") check_install 0 && status 0
        ;;
        "enable") check_install 0 && enable 0
        ;;
        "disable") check_install 0 && disable 0
        ;;
        "log") check_install 0 && show_log 0
        ;;
        "update") check_install 0 && update 0 $2
        ;;
        "config") config $*
        ;;
        "install") check_uninstall 0 && install 0
        ;;
        "uninstall") check_install 0 && uninstall 0
        ;;
        "version") check_install 0 && show_XMRay_version 0
        ;;
        "update_script") update_script
        ;;
		"warp") check_install 0 && show_XMRay_warp 0
        ;;
		"x25519") check_install 0 && show_XMRay_x25519 0
        ;;
		"mldsa65") check_install 0 && show_XMRay_mldsa65 0
        ;;
		"mlkem768") check_install 0 && show_XMRay_mlkem768 0
        ;;
		"vlessenc") check_install 0 && show_XMRay_vlessenc 0
        ;;
		"ping") check_install 0 && show_XMRay_ping 0
        ;;
		"obtain") check_install 0 && show_XMRay_certObtain 0
        ;;
		"renew") check_install 0 && show_XMRay_certRenew 0
        ;;
		"ech") check_install && show_XMRay_tls_ech 0
        ;;
		"hash") check_install && show_XMRay_tls_hash 0
        ;;
		"generate") check_install && show_XMRay_tls_generate 0
        ;;
        *) show_usage
    esac
else
    show_menu
fi