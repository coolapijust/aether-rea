const ipListEl = document.getElementById("ip-list");
const refreshButton = document.getElementById("refresh-ip");
const bestIpEl = document.getElementById("best-ip");
const copyButton = document.getElementById("copy-command");

async function fetchIpList() {
  ipListEl.innerHTML = "<div class=\"ip-row muted\">加载中...</div>";
  try {
    const response = await fetch("https://ip.v2too.top/");
    const text = await response.text();
    const ips = text
      .split(/\s+/)
      .map((value) => value.trim())
      .filter(Boolean);

    if (!ips.length) {
      ipListEl.innerHTML = "<div class=\"ip-row muted\">未找到可用 IP</div>";
      return;
    }

    renderIpList(ips.slice(0, 12));
  } catch (error) {
    ipListEl.innerHTML = "<div class=\"ip-row muted\">无法获取 IP 列表</div>";
  }
}

function renderIpList(ips) {
  ipListEl.innerHTML = "";
  ips.forEach((ip, index) => {
    const row = document.createElement("div");
    row.className = "ip-row";
    row.innerHTML = `<span>${ip}</span><strong>${index === 0 ? "建议" : "候选"}</strong>`;
    row.addEventListener("click", () => selectIp(ip, row));
    ipListEl.appendChild(row);
    if (index === 0) {
      selectIp(ip, row);
    }
  });
}

function selectIp(ip, row) {
  document.querySelectorAll(".ip-row").forEach((el) => {
    el.classList.remove("selected");
  });
  row.classList.add("selected");
  bestIpEl.textContent = ip;
}

copyButton.addEventListener("click", () => {
  const form = document.getElementById("config-form");
  const data = new FormData(form);
  const domain = data.get("domain");
  const psk = data.get("psk");
  const listen = data.get("listen");
  const rotate = data.get("rotate");
  const padding = data.get("padding");
  const bestIp = bestIpEl.textContent;

  const parts = [
    "./aether-client",
    `--url https://${domain}/v1/api/sync`,
    `--psk \"${psk || "<PSK>"}\"`,
    `--listen ${listen}`,
    `--rotate ${rotate}`,
    `--max-padding ${padding}`,
  ];

  if (bestIp && bestIp !== "未选择") {
    parts.push(`--dial-addr ${bestIp}:443`);
  }

  const command = parts.join(" ");
  navigator.clipboard.writeText(command);
  copyButton.textContent = "已复制";
  setTimeout(() => {
    copyButton.textContent = "复制启动命令";
  }, 1500);
});

refreshButton.addEventListener("click", fetchIpList);

fetchIpList();
