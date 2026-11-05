// Linux Shell 语法高亮：轻量词法扫描，输出经过 HTML 转义的 token 片段。
// 仅用于展示，不追求完整的 Shell 语法解析。

const KEYWORDS = new Set([
  "if",
  "then",
  "else",
  "elif",
  "fi",
  "for",
  "while",
  "until",
  "do",
  "done",
  "case",
  "in",
  "esac",
  "function",
  "select",
  "time",
  "return",
  "exit",
  "break",
  "continue",
  "local",
  "export",
  "readonly",
  "declare",
  "typeset",
  "unset",
  "shift",
  "trap",
  "source",
  "alias",
  "eval",
  "exec",
  "set",
  "let",
]);

/** 关键字之后仍处于命令起始位置 */
const CMD_AFTER_KEYWORD = new Set(["then", "do", "else", "elif", "if", "while", "until", "in"]);

/** 前缀命令：后面跟着真正的命令 */
const PREFIX_COMMANDS = new Set(["sudo", "env", "nohup", "time", "command", "builtin"]);

/** 会把后续内容带入命令起始位置的运算符 */
const CMD_AFTER_OP = new Set(["|", "||", "&&", ";", "(", "{"]);

const OPS = ["2>&1", "&>>", "&>", ">>", "<<", "&&", "||", "|", ";", ">", "<", "&", "(", ")", "{", "}"];

function esc(text: string): string {
  return text.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function span(cls: string, text: string): string {
  return `<span class="tk-${cls}">${esc(text)}</span>`;
}

/** 将 Shell 代码转换为带 tk-* 类名的 HTML（内容已转义） */
export function highlightShell(code: string): string {
  let out = "";
  let i = 0;
  /** 是否处于命令起始位置 */
  let cmdPos = true;

  while (i < code.length) {
    const ch = code[i];
    const rest = code.slice(i);

    if (ch === "\n") {
      out += "\n";
      cmdPos = true;
      i++;
      continue;
    }

    // 注释：行首或空白 / 运算符之后的 #
    if (ch === "#" && (i === 0 || /[\s;|&(]/.test(code[i - 1]))) {
      const nl = code.indexOf("\n", i);
      const end = nl === -1 ? code.length : nl;
      out += span("com", code.slice(i, end));
      i = end;
      continue;
    }

    // 单引号 / 双引号字符串
    if (ch === "'" || ch === '"') {
      const quote = ch;
      let j = i + 1;
      while (j < code.length && code[j] !== quote) {
        if (quote === '"' && code[j] === "\\") j++;
        j++;
      }
      const end = Math.min(j + 1, code.length);
      out += span("str", code.slice(i, end));
      i = end;
      cmdPos = false;
      continue;
    }

    // 变量与命令替换
    if (ch === "$") {
      if (code[i + 1] === "{") {
        const close = code.indexOf("}", i + 2);
        const end = close === -1 ? code.length : close + 1;
        out += span("var", code.slice(i, end));
        i = end;
      } else if (code[i + 1] === "(") {
        let depth = 1;
        let j = i + 2;
        while (j < code.length && depth > 0) {
          if (code[j] === "(") depth++;
          else if (code[j] === ")") depth--;
          j++;
        }
        out += span("var", code.slice(i, j));
        i = j;
      } else {
        const m = /^\$[A-Za-z_][A-Za-z0-9_]*|^\$[0-9@*#?$!-]/.exec(rest);
        if (m) {
          out += span("var", m[0]);
          i += m[0].length;
        } else {
          out += esc(ch);
          i++;
        }
      }
      cmdPos = false;
      continue;
    }

    // 运算符与重定向
    const op = OPS.find((o) => code.startsWith(o, i));
    if (op) {
      out += span("op", op);
      i += op.length;
      if (CMD_AFTER_OP.has(op)) cmdPos = true;
      continue;
    }

    // 空白原样保留
    if (ch === " " || ch === "\t") {
      out += ch;
      i++;
      continue;
    }

    // 变量赋值：VAR=value
    const assign = /^[A-Za-z_][A-Za-z0-9_]*=/.exec(rest);
    if (assign) {
      out += span("var", assign[0]);
      i += assign[0].length;
      cmdPos = false;
      continue;
    }

    // 命令行选项：-a / --all
    const opt = /^--?[A-Za-z][A-Za-z0-9-]*/.exec(rest);
    if (opt) {
      out += span("opt", opt[0]);
      i += opt[0].length;
      cmdPos = false;
      continue;
    }

    // 数字
    const num = /^\d+(?:\.\d+)?/.exec(rest);
    if (num) {
      out += span("num", num[0]);
      i += num[0].length;
      cmdPos = false;
      continue;
    }

    // 路径命令：/usr/bin/env、./deploy.sh
    const path = /^(?:~|\.{1,2})?\/[^\s;|&<>()]*/.exec(rest);
    if (path) {
      out += cmdPos ? span("cmd", path[0]) : esc(path[0]);
      i += path[0].length;
      cmdPos = false;
      continue;
    }

    // 单词：关键字 / 命令 / 参数
    const word = /^[A-Za-z_][A-Za-z0-9_.-]*/.exec(rest);
    if (word) {
      const w = word[0];
      if (KEYWORDS.has(w)) {
        out += span("key", w);
        cmdPos = CMD_AFTER_KEYWORD.has(w);
      } else if (cmdPos) {
        out += span("cmd", w);
        cmdPos = PREFIX_COMMANDS.has(w);
      } else {
        out += esc(w);
      }
      i += w.length;
      continue;
    }

    out += esc(ch);
    i++;
  }

  return out;
}
