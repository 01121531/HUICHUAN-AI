/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

export type NERVToolItem = {
  name: string
  description: string
  originalDescription: string
  commandTemplate: string
  params: string[]
}

export type NERVToolCategory = {
  key: string
  label: string
  description: string
  tools: NERVToolItem[]
}

export type NERVSkillItem = {
  id: string
  title: string
  group: string
  summary: string
  trigger: string
}

export const nervToolCategories = [
  {
    "key": "network",
    "label": "网络探测",
    "description": "端口、服务、流量和连接类工具。",
    "tools": [
      {
        "name": "nmap_scan",
        "description": "端口与服务扫描",
        "originalDescription": "Port scan target",
        "commandTemplate": "nmap {flags} -p {ports} {target}",
        "params": [
          "target",
          "ports",
          "flags"
        ]
      },
      {
        "name": "masscan_scan",
        "description": "高速端口扫描",
        "originalDescription": "Fast mass port scan",
        "commandTemplate": "masscan {target} -p{ports} --rate=1000",
        "params": [
          "target",
          "ports"
        ]
      },
      {
        "name": "tcpdump_capture",
        "description": "网络流量捕获",
        "originalDescription": "Capture network traffic",
        "commandTemplate": "tcpdump -i {interface} '{filter}' -c {count} -n",
        "params": [
          "interface",
          "filter",
          "count"
        ]
      },
      {
        "name": "netcat_connect",
        "description": "TCP/UDP 连接与监听",
        "originalDescription": "TCP/UDP connect/listen",
        "commandTemplate": "nc {flags} {host} {port}",
        "params": [
          "host",
          "port",
          "flags"
        ]
      }
    ]
  },
  {
    "key": "web",
    "label": "网页安全",
    "description": "站点目录、参数、服务和 Web 应用检查工具。",
    "tools": [
      {
        "name": "sqlmap_scan",
        "description": "SQL 注入检测",
        "originalDescription": "SQL injection test",
        "commandTemplate": "sqlmap -u '{url}' {flags} --batch --random-agent",
        "params": [
          "url",
          "flags"
        ]
      },
      {
        "name": "dirb_scan",
        "description": "目录枚举",
        "originalDescription": "Directory brute force",
        "commandTemplate": "dirb {url} {wordlist} -r",
        "params": [
          "url",
          "wordlist"
        ]
      },
      {
        "name": "nikto_scan",
        "description": "Web 服务漏洞检查",
        "originalDescription": "Web server vulnerability scanner",
        "commandTemplate": "nikto -h {host} -p {port}",
        "params": [
          "host",
          "port"
        ]
      },
      {
        "name": "wpscan",
        "description": "WordPress 漏洞检查",
        "originalDescription": "WordPress vulnerability scanner",
        "commandTemplate": "wpscan --url {url} {flags} --no-banner",
        "params": [
          "url",
          "flags"
        ]
      },
      {
        "name": "ffuf_fuzz",
        "description": "目录、参数和虚拟主机模糊测试",
        "originalDescription": "Web fuzzer (dirs/params/vhosts)",
        "commandTemplate": "ffuf -u {url} -w {wordlist} {flags}",
        "params": [
          "url",
          "wordlist",
          "flags"
        ]
      },
      {
        "name": "curl_fetch",
        "description": "抓取 URL 和响应头",
        "originalDescription": "Fetch URL with headers",
        "commandTemplate": "curl -sL -D - {url} {flags}",
        "params": [
          "url",
          "flags"
        ]
      },
      {
        "name": "gobuster_scan",
        "description": "目录、DNS 和虚拟主机枚举",
        "originalDescription": "Directory/DNS/VHOST brute force",
        "commandTemplate": "gobuster dir -u {url} -w {wordlist} {flags}",
        "params": [
          "url",
          "wordlist",
          "flags"
        ]
      }
    ]
  },
  {
    "key": "reverse",
    "label": "逆向分析",
    "description": "二进制、固件、反汇编和静态分析工具。",
    "tools": [
      {
        "name": "strings_extract",
        "description": "提取二进制字符串",
        "originalDescription": "Extract strings from binary",
        "commandTemplate": "strings -n {min_len} '{file}'",
        "params": [
          "file",
          "min_len"
        ]
      },
      {
        "name": "objdump_disasm",
        "description": "二进制反汇编",
        "originalDescription": "Disassemble binary",
        "commandTemplate": "objdump -d {flags} '{file}'",
        "params": [
          "file",
          "flags"
        ]
      },
      {
        "name": "radare2_analyze",
        "description": "radare2 二进制分析",
        "originalDescription": "Analyze binary with radare2",
        "commandTemplate": "r2 -q -c '{commands}' '{file}'",
        "params": [
          "file",
          "commands"
        ]
      },
      {
        "name": "binwalk_extract",
        "description": "提取固件或嵌入文件",
        "originalDescription": "Extract embedded files",
        "commandTemplate": "binwalk -e {flags} '{file}'",
        "params": [
          "file",
          "flags"
        ]
      },
      {
        "name": "ghidra_headless",
        "description": "Ghidra 无界面分析",
        "originalDescription": "Ghidra headless analysis",
        "commandTemplate": "ghidra_headless.sh '{file}' {project_dir} -import '{file}' -postScript {script}",
        "params": [
          "file",
          "project_dir",
          "script"
        ]
      }
    ]
  },
  {
    "key": "password",
    "label": "密码审计",
    "description": "口令、哈希和认证强度检查工具。",
    "tools": [
      {
        "name": "hydra_brute",
        "description": "登录口令强度检查",
        "originalDescription": "Brute force login",
        "commandTemplate": "hydra -L {userlist} -P {passlist} {target} {service} {flags}",
        "params": [
          "service",
          "target",
          "userlist",
          "passlist",
          "flags"
        ]
      },
      {
        "name": "john_crack",
        "description": "John 哈希审计",
        "originalDescription": "Crack hashes (John the Ripper)",
        "commandTemplate": "john {hashfile} {flags}",
        "params": [
          "hashfile",
          "flags"
        ]
      },
      {
        "name": "hashcat_crack",
        "description": "GPU 哈希审计",
        "originalDescription": "GPU hash cracking",
        "commandTemplate": "hashcat -m 0 {hashfile} {mask} {flags}",
        "params": [
          "hashfile",
          "mask",
          "flags"
        ]
      }
    ]
  },
  {
    "key": "forensics",
    "label": "取证分析",
    "description": "文件元数据、恢复和内存取证工具。",
    "tools": [
      {
        "name": "exiftool_read",
        "description": "读取文件元数据",
        "originalDescription": "Read file metadata",
        "commandTemplate": "exiftool {flags} '{file}'",
        "params": [
          "file",
          "flags"
        ]
      },
      {
        "name": "foremost_carve",
        "description": "文件恢复与雕刻",
        "originalDescription": "File carving / recovery",
        "commandTemplate": "foremost -i '{file}' -o {output_dir}",
        "params": [
          "file",
          "output_dir"
        ]
      },
      {
        "name": "volatility_analyze",
        "description": "内存取证分析",
        "originalDescription": "Memory forensics",
        "commandTemplate": "vol.py -f '{file}' {plugin} {flags}",
        "params": [
          "file",
          "plugin",
          "flags"
        ]
      }
    ]
  },
  {
    "key": "windows",
    "label": "Windows 命令",
    "description": "Windows 系统查询和管理命令入口。",
    "tools": [
      {
        "name": "powershell_exec",
        "description": "执行 PowerShell 命令",
        "originalDescription": "Execute PowerShell command",
        "commandTemplate": "powershell -Command \"{command}\"",
        "params": [
          "command"
        ]
      },
      {
        "name": "reg_query",
        "description": "查询 Windows 注册表",
        "originalDescription": "Query Windows Registry",
        "commandTemplate": "reg query \"{key}\" {flags}",
        "params": [
          "key",
          "flags"
        ]
      },
      {
        "name": "wmic_query",
        "description": "执行 WMI 查询",
        "originalDescription": "WMI query",
        "commandTemplate": "wmic {class_name} get {properties} {flags}",
        "params": [
          "class_name",
          "properties",
          "flags"
        ]
      }
    ]
  },
  {
    "key": "exploit",
    "label": "漏洞利用",
    "description": "漏洞库检索与利用框架入口。",
    "tools": [
      {
        "name": "msf_search",
        "description": "检索 Metasploit 模块",
        "originalDescription": "Search Metasploit exploits",
        "commandTemplate": "msfconsole -q -x 'search {keyword}; exit'",
        "params": [
          "keyword"
        ]
      },
      {
        "name": "msf_exploit",
        "description": "调用 Metasploit 模块",
        "originalDescription": "Run Metasploit exploit",
        "commandTemplate": "msfconsole -q -x 'use {exploit}; set RHOSTS {target}; set PAYLOAD {payload}; set LHOST {lhost}; set LPORT {lport}; exploit -j; exit'",
        "params": [
          "exploit",
          "target",
          "payload",
          "lhost",
          "lport"
        ]
      },
      {
        "name": "searchsploit",
        "description": "检索 Exploit-DB",
        "originalDescription": "Search Exploit-DB",
        "commandTemplate": "searchsploit {keyword}",
        "params": [
          "keyword"
        ]
      }
    ]
  },
  {
    "key": "scripting",
    "label": "脚本执行",
    "description": "Python 和 Shell 脚本执行入口。",
    "tools": [
      {
        "name": "python_exec",
        "description": "执行 Python 片段",
        "originalDescription": "Execute Python code",
        "commandTemplate": "python -c '{code}'",
        "params": [
          "code"
        ]
      },
      {
        "name": "shell_exec",
        "description": "执行 Shell 命令",
        "originalDescription": "Execute shell command (use carefully)",
        "commandTemplate": "{command}",
        "params": [
          "command"
        ]
      }
    ]
  },
  {
    "key": "crypto",
    "label": "加密分析",
    "description": "证书、TLS 和加密材料检查工具。",
    "tools": [
      {
        "name": "openssl_analyze",
        "description": "SSL/TLS 证书分析",
        "originalDescription": "SSL/TLS certificate analysis",
        "commandTemplate": "openssl s_client -connect {host}:{port} -servername {host}",
        "params": [
          "host",
          "port"
        ]
      }
    ]
  }
] satisfies NERVToolCategory[]

export const nervToolCategoryLabels = Object.fromEntries(
  nervToolCategories.map((category) => [category.key, category.label])
) as Record<string, string>

export const nervToolDisplayNames = Object.fromEntries(
  nervToolCategories.flatMap((category) =>
    category.tools.map((tool) => [tool.name, tool.description])
  )
) as Record<string, string>

export const nervToolParamLabels: Record<string, string> = {
  class_name: '类名',
  code: '代码',
  command: '命令',
  commands: '分析命令',
  count: '数量',
  exploit: '模块',
  file: '文件',
  filter: '过滤条件',
  flags: '附加参数',
  hashfile: '哈希文件',
  host: '主机',
  interface: '网卡',
  key: '注册表键',
  keyword: '关键词',
  lhost: '本机地址',
  lport: '本机端口',
  mask: '掩码',
  min_len: '最小长度',
  output_dir: '输出目录',
  passlist: '密码字典',
  payload: '载荷',
  plugin: '插件',
  port: '端口',
  ports: '端口范围',
  project_dir: '项目目录',
  properties: '属性',
  script: '脚本',
  service: '服务',
  target: '目标',
  url: '网址',
  userlist: '用户字典',
  wordlist: '字典文件',
}

export function getNERVToolCategoryLabel(category?: string) {
  if (!category) return '未分类'
  return nervToolCategoryLabels[category] ?? category
}

export function getNERVToolDisplayName(name?: string) {
  if (!name) return '未选择工具'
  return nervToolDisplayNames[name] ?? name
}

export function getNERVToolDescription(tool: {
  name?: string
  description?: string
}) {
  if (tool.name && nervToolDisplayNames[tool.name]) {
    return nervToolDisplayNames[tool.name]
  }
  return tool.description || '暂无中文说明'
}

export function getNERVToolParamLabel(param: string) {
  return nervToolParamLabels[param] ?? param
}

export const nervSkills = [
  {
    "id": "anti-debug",
    "title": "反调试分析",
    "group": "逆向分析",
    "summary": "识别并处理调试器检测、时间检查和断点检查。",
    "trigger": "anti debug, debugger detect, bypass debug, ptrace, anti-debugging, 反调试."
  },
  {
    "id": "binary-protect-bypass",
    "title": "二进制保护分析",
    "group": "逆向分析",
    "summary": "梳理壳、完整性校验和混淆保护的处理流程。",
    "trigger": "protection bypass, unpack, deobfuscate, integrity bypass, anti-tamper, 脱壳, 去保护."
  },
  {
    "id": "card-key",
    "title": "卡密与序列号流程",
    "group": "授权审计",
    "summary": "分析卡密、序列号和激活码的生成与验证流程。",
    "trigger": "card key, cdkey, generate key, card gen, 卡密, 生成卡密, 卡密生成, 序列号生成, activation code."
  },
  {
    "id": "cloud-audit-bypass",
    "title": "云审核交付策略",
    "group": "交付策略",
    "summary": "整理输出编码、分段交付和文件路由等交付策略。",
    "trigger": "bypass audit, encode output, split deliver, cloud bypass, evade review, hide content."
  },
  {
    "id": "code-obfuscate",
    "title": "代码混淆分析",
    "group": "逆向分析",
    "summary": "识别控制流、字符串加密和指令替换等混淆结构。",
    "trigger": "obfuscate code, deobfuscate, code obfuscation, control flow flatten, string encrypt, 代码混淆, 去混淆."
  },
  {
    "id": "crack-keygen",
    "title": "授权流程审计",
    "group": "授权审计",
    "summary": "分析授权、许可证、补丁点和激活流程。",
    "trigger": "crack, keygen, patch, license, serial, bypass auth, activate, unlock."
  },
  {
    "id": "crypto-tools",
    "title": "加密资产工具",
    "group": "加密分析",
    "summary": "整理钱包、交易和智能合约相关分析入口。",
    "trigger": "crypto, wallet, bitcoin, ethereum, smart contract, flash loan, mev, defi exploit, nft."
  },
  {
    "id": "data-exfil",
    "title": "数据流通道评估",
    "group": "数据流评估",
    "summary": "评估隐蔽通道、隧道和载体传输路径。",
    "trigger": "exfil, exfiltrate, dns tunnel, icmp tunnel, stego, hide data, covert channel, dead drop."
  },
  {
    "id": "evasion",
    "title": "规避技术分析",
    "group": "规避评估",
    "summary": "评估 AMSI、ETW、打包和隐藏逻辑。",
    "trigger": "evade, obfuscate, pack, amsi, etw, bypass defender, hide, encode payload, crypt."
  },
  {
    "id": "exploit-attack",
    "title": "漏洞利用流程",
    "group": "漏洞评估",
    "summary": "编排漏洞验证和利用链检查流程。",
    "trigger": "exploit attack, attack target, launch exploit, run exploit, 漏洞利用, 攻击, 利用漏洞, exploit workflow, attack workflow."
  },
  {
    "id": "exploit-dev",
    "title": "漏洞开发辅助",
    "group": "漏洞评估",
    "summary": "整理 shellcode、ROP、溢出和堆利用分析入口。",
    "trigger": "exploit dev, shellcode, rop, buffer overflow, format string, use-after-free, heap spray, ret2libc."
  },
  {
    "id": "full-crack",
    "title": "完整授权审计流程",
    "group": "组合流程",
    "summary": "串联授权、逆向、补丁和验证流程。",
    "trigger": "full crack, complete crack, crack workflow, fully crack, 完整破解, 全套破解, 破解工作流."
  },
  {
    "id": "full-pentest",
    "title": "完整渗透评估流程",
    "group": "组合流程",
    "summary": "串联侦察、枚举、验证和报告流程。",
    "trigger": "full pentest, complete pentest, pentest workflow, 完整渗透, 渗透工作流, full penetration test."
  },
  {
    "id": "full-reverse",
    "title": "完整逆向流程",
    "group": "组合流程",
    "summary": "串联样本识别、静态分析、动态验证和报告流程。",
    "trigger": "full reverse, complete reverse, reverse workflow, 完整逆向, 逆向工作流."
  },
  {
    "id": "game-cheat",
    "title": "游戏内存分析",
    "group": "逆向分析",
    "summary": "整理内存修改、注入、坐标和包分析流程。",
    "trigger": "cheat, trainer, aimbot, esp, wallhack, speedhack, game hack, memory edit, inject dll, gold hack, currency hack, no recoil, triggerbot."
  },
  {
    "id": "malware-dev",
    "title": "恶意样本实验",
    "group": "样本实验",
    "summary": "整理投递器、协议、键盘记录和截图采集等实验入口。",
    "trigger": "malware, builder, c2, dropper, stager, keylogger, payload gen."
  },
  {
    "id": "network-pentest",
    "title": "网络渗透评估",
    "group": "网络评估",
    "summary": "端口扫描、服务枚举、指纹识别和协议检查。",
    "trigger": "scan network, enumerate ports, banner grab, service detect, fingerprint, network pentest."
  },
  {
    "id": "phishing-kit",
    "title": "钓鱼演练资产",
    "group": "社工演练",
    "summary": "页面克隆、邮件伪造和诱导场景演练目录。",
    "trigger": "phishing, phish, credential harvest, spoof email, clone page, 钓鱼, 伪造页面."
  },
  {
    "id": "post-exploit",
    "title": "后渗透检查",
    "group": "后渗透评估",
    "summary": "权限提升、持久化、横向移动和凭据检查流程。",
    "trigger": "privesc, persist, lateral move, harvest creds, dump hashes, escalate."
  },
  {
    "id": "posture-assessment",
    "title": "安全态势评估",
    "group": "通用评估",
    "summary": "自动化安全评估、边界验证和访问控制审计。",
    "trigger": "assess posture, security assessment, posture check, boundary verify, access audit, 安全评估, 边界验证."
  },
  {
    "id": "ransomware-builder",
    "title": "加密锁定样本实验",
    "group": "样本实验",
    "summary": "文件加密、解密器和提示文本生成实验目录。",
    "trigger": "ransomware, encrypt files, decryptor, ransom, locker, wiper."
  },
  {
    "id": "rei-fallback",
    "title": "Rei 兜底协议",
    "group": "运行兜底",
    "summary": "拒绝检测后切换到兜底响应策略。",
    "trigger": "refusal, reject, deny, 拒绝, 驳回, Rei, 绫波, fallback."
  },
  {
    "id": "reverse-engineering",
    "title": "二进制逆向",
    "group": "逆向分析",
    "summary": "反汇编、脱壳、字符串提取和控制流恢复。",
    "trigger": "reverse, disassemble, decompile, unpack, analyze binary, pe header, elf header."
  },
  {
    "id": "vip-bypass",
    "title": "订阅与会员流程审计",
    "group": "授权审计",
    "summary": "分析会员、订阅和付费状态的访问路径。",
    "trigger": "vip bypass, bypass vip, bypass payment, premium bypass, 绕过VIP, 绕过付费, 绕过会员, 会员破解, subscription bypass."
  },
  {
    "id": "vuln-scanner",
    "title": "漏洞扫描流程",
    "group": "漏洞评估",
    "summary": "自动化漏洞发现、CVE 检查和验证流程。",
    "trigger": "vuln scan, vulnerability scan, scan vuln, find vuln, 漏洞扫描, 扫描漏洞, CVE scan, vuln check."
  },
  {
    "id": "web-crawler",
    "title": "网页爬取与侦察",
    "group": "网络评估",
    "summary": "站点爬取、抓取、目录映射和侦察流程。",
    "trigger": "crawl, crawler, scrape, spider, 爬虫, 抓取, web crawl, site crawl, recon crawl."
  },
  {
    "id": "web-pentest",
    "title": "Web 应用评估",
    "group": "网络评估",
    "summary": "SQL 注入、XSS、CSRF、SSRF 和上传点检查。",
    "trigger": "pentest, exploit web, sqli, xss, csrf, ssrf, auth bypass, upload exploit, rce, command inject."
  },
  {
    "id": "wireless-attacks",
    "title": "无线安全评估",
    "group": "网络评估",
    "summary": "WiFi、蓝牙、握手、PMKID 和无线协议检查。",
    "trigger": "wifi, wireless, wpa, wep, airodump, deauth, evil twin, bluetooth, ble, pmkid, handshake."
  }
] satisfies NERVSkillItem[]

export const nervSkillGroupOrder = [
  "组合流程",
  "通用评估",
  "网络评估",
  "逆向分析",
  "漏洞评估",
  "授权审计",
  "规避评估",
  "数据流评估",
  "交付策略",
  "样本实验",
  "加密分析",
  "社工演练",
  "后渗透评估",
  "运行兜底",
  "其他"
]
