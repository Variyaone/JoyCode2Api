package dashboard

import (
	"net/http"
	"time"

	"github.com/vibe-coding-labs/JoyCode2Api/pkg/pricing"
)

func (h *Handler) handleCosts(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, 405, "method not allowed")
		return
	}
	rows, err := h.store.GetCostRows()
	if err != nil {
		writeError(w, 500, "费用统计暂不可用")
		return
	}
	start := ""
	if len(rows) > 0 {
		start = rows[len(rows)-1].Day
	}
	writeJSON(w, 200, map[string]interface{}{
		"currency": "USD", "unit": "USD / 1M tokens", "price_version": pricing.Version,
		"collected_at": pricing.CollectedAt, "today": time.Now().Format("2006-01-02"),
		"timezone": time.Now().Format("MST -07:00"), "coverage_start": start,
		"rates": pricing.Rates, "rows": rows,
		"notice": "公开基础标价估算，非 JoyCode 实际账单。仅按已记录输入/输出 tokens；未计缓存优惠、缓存写入、长上下文/区域附加费及搜索等工具费。历史数据用引入账本时价格回溯；已删除日志无法恢复。未知价格/缺失用量不视为免费，估算并非实际费用的上界或下界。",
	})
}
