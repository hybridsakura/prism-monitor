// Prism Monitor 前端交互：SSE 实时接收 + ECharts 暗色渲染 + 历史趋势切换
(function () {
  'use strict';

  // 状态
  let currentView = 'overview';
  let historyRange = '1h';
  const MAX_POINTS = 30; // 实时图保留最近 30 个点（60s）

  const ring = {
    cpuTime: [],
    cpuTotal: [],
    cpuSteal: [],
    cpuIowait: [],
    netTime: [],
    netRx: [],
    netTx: [],
    cpuSpark: [],
    memSpark: [],
    diskSpark: [],
    netSpark: []
  };

  // ECharts 实例缓存
  let chartCpu = null;
  let chartNet = null;
  let chartHistCpu = null;
  let chartHistMem = null;

  // DOM 元素引用
  const el = id => document.getElementById(id);

  // 初始化入口
  window.addEventListener('DOMContentLoaded', () => {
    initNavigation();
    initCharts();
    initSSE();
    window.addEventListener('resize', () => {
      [chartCpu, chartNet, chartHistCpu, chartHistMem].forEach(c => c && c.resize());
    });
  });

  // 导航切换
  function initNavigation() {
    document.querySelectorAll('.nav-item').forEach(item => {
      item.addEventListener('click', e => {
        e.preventDefault();
        const view = item.dataset.view;
        switchView(view);
      });
    });

    const rangeSel = el('timeRangeSelect');
    if (rangeSel) {
      rangeSel.addEventListener('change', e => {
        historyRange = e.target.value;
        loadHistory();
      });
    }
  }

  function switchView(view) {
    currentView = view;
    document.querySelectorAll('.nav-item').forEach(n => {
      n.classList.toggle('active', n.dataset.view === view);
    });
    document.querySelectorAll('.view-container').forEach(c => {
      c.classList.toggle('active', c.id === 'view-' + view);
    });

    const isHist = view === 'history';
    el('rangeChipWrapper').style.display = isHist ? 'flex' : 'none';
    el('pageTitle').textContent = isHist ? '历史趋势' : '总览';
    el('pageSub').textContent = isHist ? '按选定时间跨度聚合查询' : '实时监控 · 每 2s 刷新';

    if (isHist) {
      setTimeout(() => {
        if (!chartHistCpu) initHistoryCharts();
        loadHistory();
        chartHistCpu && chartHistCpu.resize();
        chartHistMem && chartHistMem.resize();
      }, 50);
    } else {
      setTimeout(() => {
        chartCpu && chartCpu.resize();
        chartNet && chartNet.resize();
      }, 50);
    }
  }

  // 暗色 ECharts 基础配置
  function darkBaseOption() {
    return {
      backgroundColor: 'transparent',
      animation: false,
      grid: { left: 40, right: 16, top: 20, bottom: 25 },
      tooltip: {
        trigger: 'axis',
        backgroundColor: '#18181B',
        borderColor: '#27272A',
        textStyle: { color: '#FAFAFA', fontSize: 11 }
      },
      xAxis: {
        type: 'category',
        data: [],
        axisLine: { lineStyle: { color: '#27272A' } },
        axisLabel: { color: '#71717A', fontSize: 10 },
        splitLine: { show: false }
      },
      yAxis: {
        type: 'value',
        axisLine: { show: false },
        axisLabel: { color: '#71717A', fontSize: 10 },
        splitLine: { lineStyle: { color: '#1F1F23' } }
      }
    };
  }

  // 初始化总览图表
  function initCharts() {
    const optCpu = darkBaseOption();
    optCpu.yAxis.max = 100;
    optCpu.series = [
      {
        name: '使用率', type: 'line', data: [], smooth: true, showSymbol: false,
        lineStyle: { color: '#8B5CF6', width: 2 },
        areaStyle: { color: 'rgba(139, 92, 246, 0.12)' }
      },
      {
        name: 'Steal', type: 'line', data: [], smooth: true, showSymbol: false,
        lineStyle: { color: '#F59E0B', width: 1.5 }
      },
      {
        name: 'iowait', type: 'line', data: [], smooth: true, showSymbol: false,
        lineStyle: { color: '#F43F5E', width: 1.5 }
      }
    ];
    chartCpu = echarts.init(el('cpuChart'));
    chartCpu.setOption(optCpu);

    const optNet = darkBaseOption();
    optNet.yAxis.axisLabel.formatter = v => (v >= 1000 ? (v / 1000).toFixed(0) + 'M' : v + 'k');
    optNet.series = [
      {
        name: '下行 ↓', type: 'line', data: [], smooth: true, showSymbol: false,
        lineStyle: { color: '#8B5CF6', width: 2 },
        areaStyle: { color: 'rgba(139, 92, 246, 0.12)' }
      },
      {
        name: '上行 ↑', type: 'line', data: [], smooth: true, showSymbol: false,
        lineStyle: { color: '#22D3EE', width: 1.8 }
      }
    ];
    chartNet = echarts.init(el('netChart'));
    chartNet.setOption(optNet);
  }

  // 初始化历史图表
  function initHistoryCharts() {
    const c1 = el('historyCpuChart');
    const c2 = el('historyMemChart');
    if (!c1 || !c2) return;

    chartHistCpu = echarts.init(c1);
    const opt1 = darkBaseOption();
    opt1.yAxis.max = 100;
    opt1.legend = { data: ['均值', '最大值', '最小值', 'Steal'], textStyle: { color: '#A1A1AA' }, top: 0 };
    opt1.series = [
      { name: '均值', type: 'line', data: [], smooth: true, lineStyle: { color: '#8B5CF6', width: 2 }, areaStyle: { color: 'rgba(139, 92, 246, 0.15)' } },
      { name: '最大值', type: 'line', data: [], lineStyle: { color: '#F43F5E', type: 'dashed', width: 1.2 } },
      { name: '最小值', type: 'line', data: [], lineStyle: { color: '#10B981', type: 'dashed', width: 1.2 } },
      { name: 'Steal', type: 'line', data: [], smooth: true, lineStyle: { color: '#F59E0B', width: 1.5 } }
    ];
    chartHistCpu.setOption(opt1);

    chartHistMem = echarts.init(c2);
    const opt2 = darkBaseOption();
    opt2.yAxis.max = 100;
    opt2.legend = { data: ['内存 %', 'SWAP %'], textStyle: { color: '#A1A1AA' }, top: 0 };
    opt2.series = [
      { name: '内存 %', type: 'line', data: [], smooth: true, lineStyle: { color: '#10B981', width: 2 }, areaStyle: { color: 'rgba(16, 185, 129, 0.12)' } },
      { name: 'SWAP %', type: 'line', data: [], smooth: true, lineStyle: { color: '#22D3EE', width: 1.8 } }
    ];
    chartHistMem.setOption(opt2);
  }

  // 加载历史数据
  async function loadHistory() {
    if (!chartHistCpu) return;
    chartHistCpu.showLoading({ color: '#8B5CF6', textColor: '#A1A1AA', maskColor: 'rgba(20,20,23,0.6)' });
    chartHistMem.showLoading({ color: '#10B981', textColor: '#A1A1AA', maskColor: 'rgba(20,20,23,0.6)' });

    try {
      const [cpuRes, stealRes, memRes, swapRes] = await Promise.all([
        fetch(`/api/v1/history?metric=cpu.total&range=${historyRange}`).then(r => r.json()),
        fetch(`/api/v1/history?metric=cpu.steal&range=${historyRange}`).then(r => r.json()),
        fetch(`/api/v1/history?metric=mem.pct&range=${historyRange}`).then(r => r.json()),
        fetch(`/api/v1/history?metric=swap.pct&range=${historyRange}`).then(r => r.json())
      ]);

      const formatTs = ts => {
        const d = new Date(ts * 1000);
        return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
      };

      const pts = cpuRes.points || [];
      const times = pts.map(p => formatTs(p.ts));
      chartHistCpu.setOption({
        xAxis: { data: times },
        series: [
          { name: '均值', data: pts.map(p => p.avg.toFixed(1)) },
          { name: '最大值', data: pts.map(p => p.max.toFixed(1)) },
          { name: '最小值', data: pts.map(p => p.min.toFixed(1)) },
          { name: 'Steal', data: (stealRes.points || []).map(p => p.avg.toFixed(1)) }
        ]
      });
      el('historyCpuMeta').textContent = `${pts.length} 个点 · 采样层 [${cpuRes.layer}]`;

      const memPts = memRes.points || [];
      const swapPts = swapRes.points || [];
      chartHistMem.setOption({
        xAxis: { data: memPts.map(p => formatTs(p.ts)) },
        series: [
          { name: '内存 %', data: memPts.map(p => p.avg.toFixed(1)) },
          { name: 'SWAP %', data: swapPts.map(p => p.avg.toFixed(1)) }
        ]
      });
      el('historyMemMeta').textContent = `${memPts.length} 个点 · 采样层 [${memRes.layer}]`;
    } catch (e) {
      console.error('加载历史失败:', e);
    } finally {
      chartHistCpu.hideLoading();
      chartHistMem.hideLoading();
    }
  }

  // SSE 实时连接
  function initSSE() {
    const es = new EventSource('/api/v1/stream');
    const badge = el('liveBadge');
    const liveText = el('liveText');

    es.onopen = () => {
      badge.classList.remove('offline');
      liveText.textContent = 'LIVE';
    };

    es.onerror = () => {
      badge.classList.add('offline');
      liveText.textContent = 'RETRY';
    };

    es.onmessage = e => {
      try {
        const snap = JSON.parse(e.data);
        renderSnapshot(snap);
      } catch (err) {
        console.error('解析快照失败:', err);
      }
    };
  }

  // 渲染一次快照
  function renderSnapshot(s) {
    // 主机信息
    el('hostName').textContent = s.host.hostname || 'localhost';
    el('hostMeta').textContent = s.host.os || 'Linux';
    el('uptimeText').textContent = `在线 · 已运行 ${formatUptime(s.uptime)}`;

    // 1. CPU 卡
    el('cpuValue').textContent = s.cpu.total.toFixed(1) + '%';
    const cpuDelta = el('cpuDelta');
    if (s.cpu.steal > 5.0) {
      cpuDelta.textContent = `Steal ${s.cpu.steal.toFixed(1)}%`;
      cpuDelta.className = 'delta-chip warn';
    } else {
      cpuDelta.textContent = `正常`;
      cpuDelta.className = 'delta-chip';
    }
    const cores = s.cpu.cores ? s.cpu.cores.length : 1;
    const loads = s.cpu.load ? s.cpu.load.map(v => v.toFixed(2)).join(' / ') : '--';
    el('cpuSub').textContent = `${cores} 核 · 负载 ${loads}`;

    // 2. 内存卡
    el('memValue').textContent = s.mem.pct.toFixed(1) + '%';
    const memDelta = el('memDelta');
    if (s.mem.pct > 85) {
      memDelta.textContent = `偏高`;
      memDelta.className = 'delta-chip warn';
    } else {
      memDelta.textContent = `正常`;
      memDelta.className = 'delta-chip';
    }
    el('memSub').textContent = `${formatBytes(s.mem.used, 1)} / ${formatBytes(s.mem.total, 1)}`;

    // 3. 磁盘 IO
    let maxDisk = null;
    if (s.disks && s.disks.length > 0) {
      maxDisk = s.disks.reduce((prev, cur) => (cur.read_bps + cur.write_bps > prev.read_bps + prev.write_bps ? cur : prev), s.disks[0]);
    }
    if (maxDisk) {
      const totalBps = maxDisk.read_bps + maxDisk.write_bps;
      el('diskTag').textContent = maxDisk.dev;
      el('diskValue').textContent = (totalBps / (1024 * 1024)).toFixed(0) + ' MB/s';
      el('diskSub').textContent = `读 ${(maxDisk.read_bps / (1024 * 1024)).toFixed(0)} · 写 ${(maxDisk.write_bps / (1024 * 1024)).toFixed(0)} MB/s · 延迟 ${maxDisk.await_ms.toFixed(1)}ms`;
    } else {
      el('diskValue').textContent = '0 MB/s';
      el('diskSub').textContent = '无活跃磁盘';
    }

    // 4. 网络卡
    let totalRx = 0, totalTx = 0, mainIf = 'eth0';
    if (s.nets && s.nets.length > 0) {
      s.nets.forEach(n => { totalRx += n.rx_bps; totalTx += n.tx_bps; });
      mainIf = s.nets[0].if;
    }
    const rxMbps = (totalRx * 8) / (1000 * 1000);
    const txMbps = (totalTx * 8) / (1000 * 1000);
    el('netTag').textContent = mainIf;
    el('netValue').textContent = (rxMbps + txMbps).toFixed(0) + ' Mbps';
    el('netSub').textContent = `↓ ${rxMbps.toFixed(0)} · ↑ ${txMbps.toFixed(0)} Mbps`;
    el('netChartTitle').textContent = `网络吞吐 · ${mainIf}`;

    // 更新实时曲线点位
    const tStr = formatClock(s.ts);
    pushPoint(ring.cpuTime, tStr);
    pushPoint(ring.cpuTotal, s.cpu.total.toFixed(1));
    pushPoint(ring.cpuSteal, s.cpu.steal.toFixed(1));
    pushPoint(ring.cpuIowait, s.cpu.iowait.toFixed(1));

    pushPoint(ring.netTime, tStr);
    pushPoint(ring.netRx, (totalRx / 1024).toFixed(0)); // KB/s
    pushPoint(ring.netTx, (totalTx / 1024).toFixed(0));

    if (currentView === 'overview') {
      chartCpu.setOption({
        xAxis: { data: ring.cpuTime },
        series: [
          { data: ring.cpuTotal },
          { data: ring.cpuSteal },
          { data: ring.cpuIowait }
        ]
      });
      chartNet.setOption({
        xAxis: { data: ring.netTime },
        series: [
          { data: ring.netRx },
          { data: ring.netTx }
        ]
      });

      // SVG Sparklines 增量绘制
      pushPoint(ring.cpuSpark, s.cpu.total);
      pushPoint(ring.memSpark, s.mem.pct);
      pushPoint(ring.diskSpark, maxDisk ? maxDisk.read_bps + maxDisk.write_bps : 0);
      pushPoint(ring.netSpark, totalRx + totalTx);
      renderSparkline(el('cpuSpark'), ring.cpuSpark, '#8B5CF6');
      renderSparkline(el('memSpark'), ring.memSpark, '#10B981');
      renderSparkline(el('diskSpark'), ring.diskSpark, '#F59E0B');
      renderSparkline(el('netSpark'), ring.netSpark, '#22D3EE');
    }

    // 5. 挂载点列表
    renderMounts(s.mounts || []);

    // 6. SWAP 面板
    el('swapPct').textContent = s.swap.pct.toFixed(0) + '%';
    const swapBar = el('swapBar');
    swapBar.style.width = Math.min(s.swap.pct, 100) + '%';
    if (s.swap.pct > 80) swapBar.className = 'progress-bar rose';
    else if (s.swap.pct > 50) swapBar.className = 'progress-bar amber';
    else swapBar.className = 'progress-bar';
    el('swapUsed').textContent = `${formatBytes(s.swap.used, 1)} / ${formatBytes(s.swap.total, 1)}`;
    el('swapIn').textContent = `${(s.swap.in_bps / (1024 * 1024)).toFixed(1)} MB/s`;
    el('swapOut').textContent = `${(s.swap.out_bps / (1024 * 1024)).toFixed(1)} MB/s`;
    el('swapCached').textContent = formatBytes(s.swap.cached, 0);

    // 7. ZRAM 面板
    if (s.zram && s.zram.length > 0) {
      const z = s.zram[0];
      el('zramUsed').textContent = `${formatBytes(z.used, 0)} / ${formatBytes(z.total, 0)}`;
      const pct = z.total > 0 ? (z.used * 100) / z.total : 0;
      el('zramBar').style.width = Math.min(pct, 100) + '%';
      el('zramRatio').textContent = z.ratio > 0 ? z.ratio.toFixed(1) + 'x' : '--';
      el('zramAlgo').textContent = z.algo || '--';
      el('zramDev').textContent = '/dev/' + z.dev;
      el('zramOrig').textContent = formatBytes(z.orig, 1);
    } else {
      el('zramUsed').textContent = '未启用';
      el('zramBar').style.width = '0%';
      el('zramRatio').textContent = '--';
      el('zramAlgo').textContent = '无内核支持';
      el('zramDev').textContent = '--';
      el('zramOrig').textContent = '--';
    }
  }

  // 渲染挂载点进度条
  function renderMounts(mounts) {
    const list = el('mountList');
    if (!mounts.length) {
      list.innerHTML = '<div style="color:var(--text-dim);font-size:12px;">无活跃挂载点</div>';
      el('mountSummary').textContent = '0 个挂载点';
      return;
    }
    let totalBytes = 0;
    mounts.forEach(m => totalBytes += m.total);
    el('mountSummary').textContent = `${mounts.length} 个挂载点 · 合计 ${formatBytes(totalBytes, 1)}`;

    list.innerHTML = mounts.map(m => {
      let colorClass = '';
      if (m.pct >= 90) colorClass = 'rose';
      else if (m.pct >= 80) colorClass = 'amber';
      return `
        <div class="mount-row">
          <div class="mount-info">
            <span class="mount-path">${escapeHtml(m.path)} <span style="color:var(--text-dim)">· ${escapeHtml(m.dev)}</span></span>
            <span class="mount-val" style="${m.pct >= 80 ? 'color:var(--amber);font-weight:600' : ''}">${m.pct.toFixed(0)}% · ${formatBytes(m.used, 0)} / ${formatBytes(m.total, 0)}</span>
          </div>
          <div class="progress-track">
            <div class="progress-bar ${colorClass}" style="width: ${Math.min(m.pct, 100)}%;"></div>
          </div>
        </div>
      `;
    }).join('');
  }

  // 极简 SVG Sparkline 渲染（无依赖）
  function renderSparkline(container, points, color) {
    if (!container || points.length < 2) return;
    const w = 96, h = 28;
    const max = Math.max(...points, 1);
    const min = Math.min(...points, 0);
    const span = max - min || 1;
    const step = (w - 4) / (points.length - 1);
    const coords = points.map((p, i) => {
      const x = 2 + i * step;
      const y = h - 3 - ((p - min) / span) * (h - 6);
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    });
    container.innerHTML = `
      <svg width="${w}" height="${h}" viewBox="0 0 ${w} ${h}" fill="none">
        <polyline points="${coords.join(' ')}" stroke="${color}" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/>
      </svg>
    `;
  }

  function pushPoint(arr, val) {
    arr.push(val);
    if (arr.length > MAX_POINTS) arr.shift();
  }

  function formatBytes(b, frac) {
    if (!b || b <= 0) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let idx = 0;
    let n = b;
    while (n >= 1024 && idx < units.length - 1) {
      n /= 1024;
      idx++;
    }
    return n.toFixed(frac) + (units[idx] === 'B' ? ' B' : ' ' + units[idx]);
  }

  function formatUptime(sec) {
    if (!sec || sec <= 0) return '--';
    const days = Math.floor(sec / 86400);
    const hours = Math.floor((sec % 86400) / 3600);
    if (days > 0) return `${days} 天 ${hours} 小时`;
    const mins = Math.floor((sec % 3600) / 60);
    return `${hours} 小时 ${mins} 分钟`;
  }

  function formatClock(ts) {
    const d = new Date(ts * 1000);
    return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
  }

  function pad(n) { return n < 10 ? '0' + n : '' + n; }

  function escapeHtml(s) {
    return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }
})();
