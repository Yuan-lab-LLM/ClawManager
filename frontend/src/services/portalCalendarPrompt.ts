export function portalCalendarPrompt(year: number): string {
 if (!Number.isInteger(year) || year < 2000 || year > 2100) throw new Error('请选择有效年份');
 return `请查询中国国务院办公厅正式发布的【${year}】年部分节假日安排，以及其后正式更正通知，为我的门户生成年度日历 JSON。

必须联网查证，只以中国政府网或政府网站转载的正式通知为依据；不能采用预测、民间日历或往年日期推算。若尚未公布或无法核验完整安排，请明确回答「该年度安排尚未确认」，不要输出可导入的年度 JSON。

输出严格遵循以下格式，字段名与类型不能改变：
\`\`\`json
{
  "schema_version": 1,
  "region": "CN",
  "timezone": "Asia/Shanghai",
  "year": ${year},
  "coverage": "official_full_year",
  "sources": [{
    "id": "gov-${year}",
    "title": "正式通知标题",
    "url": "https://官方通知地址",
    "published_at": "YYYY-MM-DD"
  }],
  "days": [{
    "date": "YYYY-MM-DD",
    "kind": "holiday",
    "name": "节日名称",
    "source_id": "gov-${year}"
  }]
}
\`\`\`

把示例 year 和所有日期替换为目标年份的实际值。每个指定日期单独一项：官方放假区间内的每一天都列为 holiday（包括区间内的周末）；明确调休上班的日期列为 workday；普通周末不用列。按日期升序排列，不得重复、冲突、漏补班日期或加入目标年之外的日期。按实际引用通知填写 sources，source_id 必须能关联。若官方发布更正，注明更正来源，按更正后的最终安排生成。

请先给出官方来源及各节日「放假日期/天数、补班日期」核对清单，再给一个可保存为 calendar-${year}.json 的完整 JSON 代码块。不要把核对说明放进 JSON，不添加注释、占位符、猜测日期或未知字段。跨年放假仅输出目标年内日期，并在核对清单注明另一个年份也需导入相应日期。只有确实覆盖已公布全年安排时才使用 official_full_year；部分补充文件使用 partial 并在说明中标明覆盖范围。`;
}
