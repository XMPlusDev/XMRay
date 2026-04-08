#!/bin/bash
 
red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'
 
# check root
[[ $EUID -ne 0 ]] && echo -e "${red}Error：${plain} This script must be run with the root user！\n" && exit 1

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

echo "arch: ${kernelArch}"

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

install_base() {
    if [[ x"${release}" == x"centos" ]]; then
        yum install epel-release -y
        yum install wget curl unzip tar crontabs socat -y
    else
        apt update -y
        apt install wget curl unzip tar cron socat -y
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

install_acme() {
    curl https://get.acme.sh | sh
}

install_XMPlus() {
    if [[ -e /usr/local/XMRay/ ]]; then
        rm /usr/local/XMRay/ -rf
    fi
	
	if [[ -f /usr/bin/XMRay ]]; then
		rm /usr/bin/XMRay -f
	fi
	
	if [[ -f /usr/bin/xmray ]]; then
		rm /usr/bin/xmray -f
	fi
	
    mkdir /usr/local/XMRay/ -p
	
	cd /usr/local/XMRay/

    if  [ $# == 0 ] ;then
        last_version=$(curl -Ls "https://api.github.com/repos/XMPlusDev/XMPlusServer/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
        if [[ ! -n "$last_version" ]]; then
            echo -e "${red}Failed to detect the XMRay version, it may be because of Github API limit, please try again later, or manually specify the XMRay version to install${plain}"
            exit 1
        fi
        echo -e "XMRay latest version detected：${last_version}，Start Installation"
        wget -N --no-check-certificate -O /usr/local/XMRay/XMRay-linux.zip https://github.com/XMPlusDev/XMPlusServer/releases/download/${last_version}/XMRay-linux-${kernelArch}.zip
        if [[ $? -ne 0 ]]; then
            echo -e "${red}Downloading XMRay failed，Please make sure your server can download github file${plain}"
            exit 1
        fi
    else
        last_version=$1
        url="https://github.com/XMPlusDev/XMPlusServer/releases/download/${last_version}/XMRay-linux-${kernelArch}.zip"
        echo -e "Start Installation XMRay v$1"
        wget -N --no-check-certificate -O /usr/local/XMRay/XMRay-linux.zip ${url}
        if [[ $? -ne 0 ]]; then
            echo -e "${red}Downloading XMRay v$1 failed, make sure this version exists${plain}"
            exit 1
        fi
    fi

    unzip XMRay-linux.zip
    rm XMRay-linux.zip -f
    chmod +x XMRay
	
    if [ -e "/etc/systemd/system/" ] ; then
		if [ -e "/usr/lib/systemd/system/XMRay.service" ] ; then
			systemctl stop XMRay
			systemctl disable XMRay
		    rm /etc/systemd/system/XMRay.service -f
		fi
		
		file="https://raw.githubusercontent.com/XMPlusDev/XMRay/script/XMRay.service"
		wget -N --no-check-certificate -O /etc/systemd/system/XMRay.service ${file}
		systemctl daemon-reload
		systemctl stop XMRay
		systemctl enable XMRay
    elif [ -e "/usr/sbin/rc-service" ] ; then
		if [ -e "/etc/init.d/xmray" ] ; then
			systemctl stop XMRay
			systemctl disable XMRay
			rm /etc/init.d/xmray/xmray.rc -f
		else	
			 mkdir /etc/init.d/xmray/ -p
		fi
		file="https://raw.githubusercontent.com/XMPlusDev/XMRay/script/xmray.rc"
		wget -N --no-check-certificate -O /etc/init.d/xmray/xmray.rc ${file}
		systemctl daemon-reload
		rc-update add xmray default 
		rc-update --update
		chmod +x /etc/init.d/xmray/xmray.rc
		ln -s /etc/XMRay /usr/local/etc/
    else
       echo "not found."
    fi	
	
    mkdir /etc/XMRay/ -p
	
    echo -e "${green}XMRay ${last_version}${plain} The installation is complete，XMRay has restarted"
	
    cp geoip.dat /etc/XMRay/
	
    cp geosite.dat /etc/XMRay/ 
	
    if [[ ! -f /etc/XMRay/dns.json ]]; then
		cp dns.json /etc/XMRay/
	fi
	if [[ ! -f /etc/XMRay/route.json ]]; then 
		cp route.json /etc/XMRay/
	fi
	
	if [[ ! -f /etc/XMRay/outbound.json ]]; then
		cp outbound.json /etc/XMRay/
	fi
	
	if [[ ! -f /etc/XMRay/inbound.json ]]; then
		cp inbound.json /etc/XMRay/
	fi
	
    if [[ ! -f /etc/XMRay/config.yml ]]; then
        cp config.yml /etc/XMRay/
    else
		if [ -e "/etc/systemd/system/" ] ; then
			systemctl start XMRay
		else
			rc-service xmray start
		fi
        sleep 2
        check_status
        echo -e ""
        if [[ $? == 0 ]]; then
            echo -e "${green}XMRay restart successfully${plain}"
        else
            echo -e "${red} XMRay May fail to start, please use [ XMRay log ] View log information ${plain}"
        fi
    fi
    
    curl -o /usr/bin/XMRay -Ls https://raw.githubusercontent.com/XMPlusDev/XMPlusServer/script/XMRay.sh
    chmod +x /usr/bin/XMRay
    ln -s /usr/bin/XMRay /usr/bin/xmray 
    chmod +x /usr/bin/xmray

    echo -e ""
    echo "XMRay Management usage method: "
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

echo -e "${green}Start Installation${plain}"
install_base
#install_acme
install_XMPlus $1
