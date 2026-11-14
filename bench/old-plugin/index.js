/**
 * 阶段零性能基准插件（旧版 TRSS-Yunzai 侧）。
 * 规则集、顺序、优先级和回复文案必须与 go-yunzai plugins/bench/bench.go
 * 保持一致；任何一侧修改都要同步另一侧，并在基线报告中标注工作负载版本。
 * 工作负载版本：1
 */
export class benchWorkload extends plugin {
  constructor() {
    super({
      name: "bench-workload",
      dsc: "阶段零基准插件，仅覆盖消息匹配与回复链路",
      event: "message",
      priority: 50,
      rule: [
        /** 三条命中规则，顺序与 Go 版一致 */
        { reg: "^#ping$", fnc: "ping" },
        { reg: "^#help$", fnc: "help" },
        { reg: "^bench (\\d+)$", fnc: "echo" },
        /** 28 条在基准负载中永不命中的真实风格命令 */
        { reg: "^#签到$", fnc: "notReached" },
        { reg: "^#体力$", fnc: "notReached" },
        { reg: "^#状态$", fnc: "notReached" },
        { reg: "^#今日运势$", fnc: "notReached" },
        { reg: "^#天气\\s+.+$", fnc: "notReached" },
        { reg: "^#菜单$", fnc: "notReached" },
        { reg: "^#角色查询\\s+.+$", fnc: "notReached" },
        { reg: "^#深境螺旋$", fnc: "notReached" },
        { reg: "^#原神卡片$", fnc: "notReached" },
        { reg: "^#星穹铁道签到$", fnc: "notReached" },
        { reg: "^#崩坏三签到$", fnc: "notReached" },
        { reg: "^#绝区零签到$", fnc: "notReached" },
        { reg: "^#米游社签到$", fnc: "notReached" },
        { reg: "^#抽卡记录\\s+.+$", fnc: "notReached" },
        { reg: "^#面板\\s+.+$", fnc: "notReached" },
        { reg: "^#更新面板$", fnc: "notReached" },
        { reg: "^#帮助列表$", fnc: "notReached" },
        { reg: "^#绑定\\s+.+$", fnc: "notReached" },
        { reg: "^#解绑$", fnc: "notReached" },
        { reg: "^#深渊\\s+.+$", fnc: "notReached" },
        { reg: "^#群管\\s+.+$", fnc: "notReached" },
        { reg: "^#点歌\\s+.+$", fnc: "notReached" },
        { reg: "^#歌词\\s+.+$", fnc: "notReached" },
        { reg: "^#翻译\\s+.+$", fnc: "notReached" },
        { reg: "^#百科\\s+.+$", fnc: "notReached" },
        { reg: "^#新闻$", fnc: "notReached" },
        { reg: "^#日历$", fnc: "notReached" },
        { reg: "^#汇率\\s+.+$", fnc: "notReached" },
      ],
    })
  }

  async ping() {
    return this.reply("[bench] pong")
  }

  async help() {
    return this.reply("[bench] 帮助：阶段零基准规则集 v1（ping/help/bench + 28 条扫描规则）")
  }

  async echo() {
    return this.reply(`[bench] ${this.e.msg.match(/^bench (\d+)$/)[1]}`)
  }

  /** 永不命中的规则占位；如被调用说明负载文本污染了规则集。 */
  async notReached() {
    throw new Error("bench 扫描规则不应命中")
  }
}
