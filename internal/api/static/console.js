"use strict";

// Kept as data so the Go asset test can ask the real mux about each static
// route the browser uses. Dynamic job URLs are expressed by the functions
// below and covered by synthesised job ids in that same test.
const apiRoutes = Object.freeze({
  commands: "/api/v1/commands",
  parse: "/api/v1/parse",
  complete: "/api/v1/complete",
  jobs: "/api/v1/jobs",
  session: "/api/v1/session",
  setupStart: "/api/v1/setup/start",
  setupInput: "/api/v1/setup/input"
});

const transcript = document.querySelector("#transcript");
const status = document.querySelector("#console-status");
const summary = document.querySelector("#command-summary");
const line = document.querySelector("#line");
const suggestions = document.querySelector("#suggestions");
const cancel = document.querySelector("#cancel");
const form = document.querySelector("#command-form");
const legacyHistoryKey = "dmtx-console-history";
const historyKey = "dmtx-console-history-v2";
// Run records are durable operator history, not an unbounded log viewer. A
// compromised or future server must not be able to make a recovered console
// append arbitrarily many or arbitrarily large transcript entries.
const maxRenderedRuns = 50;
const maxRenderedFieldLength = 512;
const maxProgressTableNames = 12;

let commands = [];
let displayed = [];
let activeSuggestion = -1;
let history = loadHistory();
let historyIndex = -1;
let historyDraft = "";
let completionGeneration = 0;
let activeJob = null;
let cancelling = false;
let setupActive = false;
let setupMasked = false;
const watchedJobs = new Set();
let transcriptLines = [];

function jobURL(id) { return apiRoutes.jobs + "/" + encodeURIComponent(id); }
function jobEventsURL(id) { return jobURL(id) + "/events"; }
function jobCancelURL(id) { return jobURL(id) + "/cancel"; }

function appendTranscript(value, kind = "") {
  const text = String(value);
  const entry = document.createElement("pre");
  entry.className = "transcript-line" + (kind ? " transcript-" + kind : "");
  entry.textContent = text;
  transcript.append(entry);
  transcriptLines.push(text);
}

function setStatus(value) { status.textContent = String(value); }

function clearTranscript() {
  transcript.replaceChildren();
  transcriptLines = [];
  setStatus("Transcript cleared.");
}

function boundedPayloadText(value) {
  if (typeof value !== "string") return "";
  return value.length > maxRenderedFieldLength
    ? value.slice(0, maxRenderedFieldLength - 1) + "…"
    : value;
}

function payloadRecord(value) {
  return value && typeof value === "object" && !Array.isArray(value) ? value : null;
}

function payloadText(record, name) {
  return record ? boundedPayloadText(record[name]) : "";
}

function payloadNumber(record, name) {
  return record && Number.isFinite(record[name]) ? String(record[name]) : "";
}

function payloadBoolean(record, name) {
  return record && typeof record[name] === "boolean" ? (record[name] ? "yes" : "no") : "";
}

function payloadList(record, name, limit = 12) {
  if (!record || !Array.isArray(record[name])) return [];
  return record[name].slice(0, limit).map(boundedPayloadText).filter(Boolean);
}

function addPayloadValue(lines, label, value) {
  if (value) lines.push(label + ": " + value);
}

function progressCount(record, name) {
  return record && Number.isSafeInteger(record[name]) && record[name] >= 0 ? record[name] : null;
}

// renderProgress accepts only the public app.Progress events. EventSource data
// is text, so parsing and validating it here prevents a server response or
// proxy error from becoming a raw JSON transcript dump.
function renderProgress(data) {
  let event;
  try {
    event = JSON.parse(data);
  } catch (_) {
    appendTranscript("Progress update unavailable.");
    return;
  }
  const record = payloadRecord(event);
  const done = progressCount(record, "done");
  const total = progressCount(record, "total");
  if (!record || done === null || total === null || done > total) {
    appendTranscript("Progress update unavailable.");
    return;
  }
  switch (record.kind) {
    case "tables_planned": {
      if (done !== 0 || !Array.isArray(record.tables) || total !== record.tables.length) break;
      const names = record.tables.slice(0, maxProgressTableNames);
      if (!names.every(name => typeof name === "string" && name)) break;
      const shown = names.map(boundedPayloadText).filter(Boolean);
      if (shown.length !== names.length) break;
      let text = "Planned " + total + " table(s).";
      if (shown.length) text += "\nTables: " + shown.join(", ");
      if (total > shown.length) text += "\nAdditional table names omitted.";
      appendTranscript(text);
      return;
    }
    case "table_started": {
      const table = boundedPayloadText(record.table);
      if (!table || total < 1) break;
      appendTranscript("Starting table " + table + " (" + done + " of " + total + " completed).");
      return;
    }
    case "table_finished": {
      const table = boundedPayloadText(record.table);
      const rows = record.rows === undefined ? 0 : progressCount(record, "rows");
      if (!table || total < 1 || done < 1 || rows === null) break;
      // Rows is the aggregate table count from app.Progress, never row data.
      appendTranscript("Finished table " + table + " (" + done + " of " + total + "; " + rows + " rows).");
      return;
    }
    default:
      appendTranscript("Progress update unavailable (unknown event).");
      return;
  }
  appendTranscript("Progress update unavailable.");
}

function renderedRun(run) {
  const record = payloadRecord(run);
  if (!record) return "";
  const value = name => payloadText(record, name);
  const lines = ["Run " + (value("id") || "(unnamed)")];
  for (const [label, field] of [
    ["Outcome", "outcome"], ["Source", "source"], ["Target", "target"],
    ["Started", "started_at"], ["Ended", "ended_at"], ["Reason", "resumability_reason"]
  ]) {
    let text = value(field);
    // Go's zero time is serialized even with omitempty. It means an active run
    // has not ended, not that it ended in year one.
    if (field === "ended_at" && text.startsWith("0001-01-01T00:00:00")) text = "";
    if (text) lines.push(label + ": " + text);
  }
  addPayloadValue(lines, "Resumable", payloadBoolean(record, "resumable"));
  return lines.join("\n");
}

function renderedStatusDetail(data) {
  const record = payloadRecord(data);
  if (!record) return "";
  const lines = ["Detailed status"];
  const run = renderedRun(record.run);
  if (run) lines.push(run);
  const tasks = Array.isArray(record.tasks) ? record.tasks.slice(0, 12) : [];
  for (const rawTask of tasks) {
    const task = payloadRecord(rawTask);
    if (!task) continue;
    const table = payloadText(task, "table");
    const taskStatus = payloadText(task, "status");
    if (!table && !taskStatus) continue;
    const facts = [];
    const rows = payloadNumber(task, "rows_done");
    const integerWatermark = payloadNumber(task, "integer_watermark");
    const rowNumberWatermark = payloadNumber(task, "row_number_watermark");
    if (rows !== "") facts.push(rows + " rows");
    if (integerWatermark !== "") facts.push("key " + integerWatermark);
    if (rowNumberWatermark !== "") facts.push("row " + rowNumberWatermark);
    lines.push("Table " + (table || "(unnamed)") + (taskStatus ? ": " + taskStatus : "") + (facts.length ? " (" + facts.join(", ") + ")" : ""));
  }
  if (Array.isArray(record.tasks) && record.tasks.length > tasks.length) lines.push("Tasks: additional entries omitted");
  return lines.join("\n");
}

// State stores lifecycle transitions. History presents runs, so select the
// latest transition for each id from the bounded recent tail while leaving the
// underlying SQLite evidence untouched.
function latestRunRecords(records) {
  const recent = records.slice(-maxRenderedRuns);
  const seen = new Set();
  const latest = [];
  for (let index = recent.length - 1; index >= 0; index--) {
    const record = payloadRecord(recent[index]);
    if (!record) continue;
    const id = typeof record.id === "string" && record.id ? record.id : "#" + index;
    if (seen.has(id)) continue;
    seen.add(id);
    latest.push(record);
  }
  latest.reverse();
  return latest;
}

function renderedResult(data, partial) {
  const record = payloadRecord(data);
  if (!record) return "";
  const lines = [partial ? "Partial migration result" : "Migration result"];
  addPayloadValue(lines, "Tables", payloadNumber(record, "tables"));
  // This is the aggregate count reported by the migration, never source row
  // values or a result-set payload.
  addPayloadValue(lines, "Rows", payloadNumber(record, "rows"));
  addPayloadValue(lines, "Validated", payloadBoolean(record, "validated"));
  if (partial) {
    addPayloadValue(lines, "Outcome", payloadText(record, "outcome"));
    addPayloadValue(lines, "Resumable", payloadBoolean(record, "resumable"));
  }
  const tuning = payloadRecord(record.runtime_tuning);
  if (tuning) {
    addPayloadValue(lines, "Runtime tuning", payloadBoolean(tuning, "enabled"));
    addPayloadValue(lines, "Tuning reason", payloadText(tuning, "reason"));
    if (Array.isArray(tuning.tables)) lines.push("Tuned tables: " + tuning.tables.length);
  }
  return lines.join("\n");
}

function renderedValidation(data) {
  const record = payloadRecord(data);
  if (!record) return "";
  const lines = ["Validation"];
  addPayloadValue(lines, "Passed", payloadBoolean(record, "passed"));
  if (!Array.isArray(record.tables)) return lines.join("\n");
  lines.push("Tables checked: " + record.tables.length);
  for (const table of record.tables.slice(0, 12)) {
    const item = payloadRecord(table);
    if (!item) continue;
    const name = payloadText(item, "table") || "(unnamed)";
    const sourceRows = payloadNumber(item, "source_rows");
    const targetRows = payloadNumber(item, "target_rows");
    const match = payloadBoolean(item, "match");
    const summary = [name];
    if (sourceRows) summary.push("source " + sourceRows);
    if (targetRows) summary.push("target " + targetRows);
    if (match) summary.push("match " + match);
    lines.push("- " + summary.join(" · "));
  }
  if (record.tables.length > 12) lines.push("- … additional tables omitted");
  return lines.join("\n");
}

function renderedPlan(data) {
  const record = payloadRecord(data);
  if (!record) return "";
  const lines = ["Dry-run plan"];
  addPayloadValue(lines, "Proceed", payloadBoolean(record, "proceed"));
  addPayloadValue(lines, "Source type", payloadText(record, "source_type"));
  addPayloadValue(lines, "Target type", payloadText(record, "target_type"));
  addPayloadValue(lines, "Target mode", payloadText(record, "target_mode"));
  if (Array.isArray(record.tables)) {
    lines.push("Tables planned: " + record.tables.length);
    for (const table of record.tables.slice(0, 12)) {
      const item = payloadRecord(table);
      if (!item) continue;
      const summary = [payloadText(item, "name"), payloadNumber(item, "rows"), payloadText(item, "rows_provenance")].filter(Boolean);
      if (summary.length) lines.push("- " + summary.join(" · "));
    }
    if (record.tables.length > 12) lines.push("- … additional tables omitted");
  }
  const admission = payloadRecord(record.admission);
  if (admission) {
    addPayloadValue(lines, "Admission supported", payloadBoolean(admission, "supported"));
    addPayloadValue(lines, "Admission", payloadText(admission, "error"));
  }
  const target = payloadRecord(record.target);
  if (target) {
    addPayloadValue(lines, "Target presence", payloadText(target, "presence"));
    addPayloadValue(lines, "Target preflight", payloadText(target, "preflight"));
    addPayloadValue(lines, "Target limitation", payloadText(target, "error"));
    for (const limitation of payloadList(target, "limitations")) lines.push("Target limitation: " + limitation);
  }
  const schema = payloadRecord(record.schema_drift);
  if (schema) {
    addPayloadValue(lines, "Schema status", payloadText(schema, "status"));
    addPayloadValue(lines, "Schema blocks proceed", payloadBoolean(schema, "blocks_proceed"));
    addPayloadValue(lines, "Schema limitation", payloadText(schema, "error"));
  }
  const deletes = payloadRecord(record.deletes);
  if (deletes) {
    addPayloadValue(lines, "Delete state", payloadText(deletes, "state_error"));
    addPayloadValue(lines, "Delete due reason", payloadText(deletes, "due_reason"));
  }
  return lines.join("\n");
}

function renderedPreflight(data) {
  const record = payloadRecord(data);
  if (!record) return "";
  const lines = ["Preflight"];
  addPayloadValue(lines, "Proceed", payloadBoolean(record, "proceed"));
  if (!Array.isArray(record.findings)) return lines.join("\n");
  lines.push("Findings: " + record.findings.length);
  for (const finding of record.findings.slice(0, 12)) {
    const item = payloadRecord(finding);
    if (!item) continue;
    const summary = [payloadText(item, "severity"), payloadText(item, "side"), payloadText(item, "check"), payloadText(item, "class")].filter(Boolean);
    if (summary.length) lines.push("- " + summary.join(" · "));
    addPayloadValue(lines, "  Finding", payloadText(item, "message"));
    addPayloadValue(lines, "  Remedy", payloadText(item, "remedy"));
    addPayloadValue(lines, "  Evidence", payloadText(item, "evidence"));
  }
  if (record.findings.length > 12) lines.push("- … additional findings omitted");
  for (const selector of payloadList(record, "skip_selectors")) lines.push("Skip selector: " + selector);
  if (Array.isArray(record.skip_selectors) && record.skip_selectors.length > 12) lines.push("Skip selectors: additional entries omitted");
  return lines.join("\n");
}

function renderedResumeResponse(data) {
  const record = payloadRecord(data);
  if (!record) return "";
  const lines = ["Resume response"];
  addPayloadValue(lines, "Run", payloadText(record, "run_id"));
  addPayloadValue(lines, "Outcome", payloadText(record, "outcome"));
  addPayloadValue(lines, "Resumable", payloadBoolean(record, "resumable"));
  return lines.join("\n");
}

function renderedConfigEndpoint(value, role) {
  const endpoint = payloadRecord(value);
  if (!endpoint) return [];
  const lines = [role + ": " + (payloadText(endpoint, "type") || "unknown")];
  addPayloadValue(lines, "  Host", payloadText(endpoint, "host"));
  addPayloadValue(lines, "  Port", payloadNumber(endpoint, "port"));
  addPayloadValue(lines, "  Database", payloadText(endpoint, "database"));
  addPayloadValue(lines, "  Schema", payloadText(endpoint, "schema"));
  addPayloadValue(lines, "  User", payloadText(endpoint, "user"));
  addPayloadValue(lines, "  SSL mode", payloadText(endpoint, "ssl_mode"));
  addPayloadValue(lines, "  TLS CA file", payloadText(endpoint, "tls_ca_file"));
  return lines;
}

function renderedConfig(data) {
  const record = payloadRecord(data);
  if (!record) return "";
  const lines = ["Configuration"];
  addPayloadValue(lines, "Path", payloadText(record, "path"));
  lines.push(...renderedConfigEndpoint(record.source, "Source"));
  lines.push(...renderedConfigEndpoint(record.target, "Target"));
  const migration = payloadRecord(record.migration);
  if (migration) {
    addPayloadValue(lines, "Target mode", payloadText(migration, "target_mode"));
    addPayloadValue(lines, "Workers", payloadNumber(migration, "workers"));
    addPayloadValue(lines, "Connection limit", payloadNumber(migration, "connection_limit"));
    if (Array.isArray(migration.include_tables)) lines.push("Included tables: " + migration.include_tables.length);
    if (Array.isArray(migration.exclude_tables)) lines.push("Excluded tables: " + migration.exclude_tables.length);
  }
  const notes = Array.isArray(record.notes) ? record.notes.slice(0, 12) : [];
  for (const note of notes) {
    const diagnostic = payloadRecord(note);
    if (!diagnostic) continue;
    const summary = [payloadText(diagnostic, "severity"), payloadText(diagnostic, "message")].filter(Boolean);
    if (summary.length) lines.push("Diagnostic: " + summary.join(": "));
  }
  if (Array.isArray(record.notes) && record.notes.length > 12) lines.push("Diagnostics: additional entries omitted");
  return lines.join("\n");
}

function renderedDiagnosis(data) {
  const record = payloadRecord(data);
  if (!record) return "";
  const lines = ["Diagnosis"];
  const run = renderedRun(record.run);
  if (run) lines.push(run);
  const tables = payloadRecord(record.tables);
  if (tables) {
    const tally = ["total", "completed", "in_progress", "not_started"].map(name => name.replace(/_/g, " ") + " " + payloadNumber(tables, name)).filter(value => !value.endsWith(" "));
    if (tally.length) lines.push("Tables: " + tally.join(", "));
  }
  const incomplete = payloadList(record, "incomplete");
  if (incomplete.length) lines.push("Incomplete: " + incomplete.join(", "));
  if (Array.isArray(record.incomplete) && record.incomplete.length > incomplete.length) lines.push("Incomplete: additional names omitted");
  for (const finding of payloadList(record, "findings")) lines.push("Finding: " + finding);
  addPayloadValue(lines, "Next step", payloadText(record, "next_step"));
  return lines.join("\n");
}

function renderedAnalysis(data) {
  const record = payloadRecord(data);
  if (!record) return "";
  const lines = ["Effective plan"];
  addPayloadValue(lines, "Path", payloadText(record, "path"));
  const tuning = payloadRecord(record.tuning);
  if (!tuning) return lines.join("\n");
  for (const [label, field] of [["Connection limit", "connection_limit"], ["Workers", "workers"], ["Readers", "readers"], ["Writers", "writers"], ["Queue depth", "queue_depth"], ["Chunk rows", "chunk_rows"], ["Memory budget", "memory_budget_bytes"]]) {
    const setting = payloadRecord(tuning[field]);
    if (!setting) continue;
    const value = payloadNumber(setting, "value");
    const provenance = payloadText(setting, "provenance");
    if (value) lines.push(label + ": " + value + (provenance ? " (" + provenance + ")" : ""));
  }
  return lines.join("\n");
}

// renderedAIAdvisory shows only the already-validated public advisory shape.
// The provider response itself never reaches this function: app.DecodeAdvisory
// rejects arbitrary model output before it becomes a payload. Keep the second
// boundary here anyway, because a browser must not turn a future payload-field
// addition into an unbounded transcript disclosure.
function renderedAIAdvisory(data) {
  const record = payloadRecord(data);
  if (!record) return "";
  const status = payloadText(record, "status");
  if (!status) return "";
  const lines = ["AI advisory", "Status: " + status];
  addPayloadValue(lines, "Provider", payloadText(record, "provider"));
  addPayloadValue(lines, "Model", payloadText(record, "model"));
  if (status !== "ok") {
    addPayloadValue(lines, "Detail", payloadText(record, "error"));
    return lines.join("\n");
  }
  const advisory = payloadRecord(record.advisory);
  if (!advisory) return lines.join("\n");
  addPayloadValue(lines, "Summary", payloadText(advisory, "summary"));
  const findings = Array.isArray(advisory.findings) ? advisory.findings.slice(0, 12) : [];
  for (const rawFinding of findings) {
    const finding = payloadRecord(rawFinding);
    if (!finding) continue;
    const heading = [payloadText(finding, "category"), payloadText(finding, "title")].filter(Boolean).join(": ");
    if (heading) lines.push("Finding: " + heading);
    addPayloadValue(lines, "  Summary", payloadText(finding, "summary"));
    addPayloadValue(lines, "  Action", payloadText(finding, "action"));
  }
  if (Array.isArray(advisory.findings) && advisory.findings.length > findings.length) lines.push("Findings: additional entries omitted");
  const warnings = Array.isArray(advisory.warnings) ? advisory.warnings.slice(0, 12) : [];
  for (const warning of warnings) {
    const text = boundedPayloadText(warning);
    if (text) lines.push("Warning: " + text);
  }
  if (Array.isArray(advisory.warnings) && advisory.warnings.length > warnings.length) lines.push("Warnings: additional entries omitted");
  return lines.join("\n");
}

// renderPayload recognizes only the bounded, public projections supplied by
// WebUI commands. It maps known fields to strings and sends them exclusively
// through appendTranscript's textContent sink: arbitrary JSON, raw SQL, and
// row values are neither interpreted nor rendered.
function renderPayload(payload) {
  if (!payload || typeof payload !== "object") return false;
  if (payload.kind === "run") {
    const text = renderedRun(payload.data);
    if (!text) return false;
    appendTranscript(text);
    return true;
  }
  if (payload.kind === "runs") {
    if (!Array.isArray(payload.data)) return false;
    if (!payload.data.length) {
      appendTranscript("No runs recorded.");
      return true;
    }
    const runs = latestRunRecords(payload.data);
    let heading = "History: " + runs.length + " run(s).";
    if (payload.data.length > maxRenderedRuns) {
      heading += " Summarized from the " + maxRenderedRuns + " most recent state records; older history omitted.";
    }
    appendTranscript(heading);
    for (const run of runs) {
      const text = renderedRun(run);
      if (text) appendTranscript(text);
    }
    return true;
  }
  const renderers = {
    status_detail: renderedStatusDetail,
    plan: renderedPlan,
    result: data => renderedResult(data, false),
    partial_result: data => renderedResult(data, true),
    validation_result: renderedValidation,
    preflight_report: renderedPreflight,
    resume_response: renderedResumeResponse,
    config: renderedConfig,
    diagnosis: renderedDiagnosis,
    analysis: renderedAnalysis,
    ai_advisory: renderedAIAdvisory
  };
  const renderer = renderers[payload.kind];
  if (!renderer) return false;
  const text = renderer(payload.data);
  if (!text) return false;
  appendTranscript(text);
  return true;
}

async function request(path, body, method) {
  const verb = method || (body === undefined ? "GET" : "POST");
  const response = await fetch(path, {
    method: verb,
    headers: body === undefined ? {} : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
    credentials: "same-origin"
  });
  const text = await response.text();
  let result = {};
  if (text) {
    try {
      result = JSON.parse(text);
    } catch (_) {
      throw new Error("server returned an invalid response");
    }
  }
  if (!response.ok) throw new Error(result.error || "request failed");
  return result;
}

function closePalette() {
  // A path-completion fetch may resolve after Escape or a selection. Advancing
  // this generation makes such an old answer unable to reopen the popup.
  completionGeneration++;
  displayed = [];
  activeSuggestion = -1;
  suggestions.replaceChildren();
  suggestions.hidden = true;
  line.setAttribute("aria-expanded", "false");
  line.removeAttribute("aria-activedescendant");
}

function firstWord(value) {
  return value.trimStart().split(/[ \t]/, 1)[0] || "";
}

function commandCompletionContext() {
  const match = /^([ \t]*)([^ \t]*)/.exec(line.value);
  return {
    leading: match[1],
    token: match[2],
    suffix: line.value.slice(match[0].length)
  };
}

// consoleWords is deliberately limited to the same non-shell quoting grammar
// as the server parser. It exists only for browser-owned commands (/session
// and setup routing); every migration command still reaches /api/v1/parse.
function consoleWords(value) {
  const input = value.trim();
  if (/\r|\n/.test(input)) return { error: "input holds more than one line" };
  const words = [];
  let current = "";
  let quote = "";
  let started = false;
  let escaped = false;
  for (const character of input) {
    if (escaped) {
      current += character;
      escaped = false;
    } else if (quote === '"' && character === "\\") {
      escaped = true;
    } else if (quote) {
      if (character === quote) quote = "";
      else current += character;
    } else if (character === "'" || character === '"') {
      quote = character;
      started = true;
    } else if (character === " " || character === "\t") {
      if (started) {
        words.push(current);
        current = "";
        started = false;
      }
    } else {
      current += character;
      started = true;
    }
  }
  if (quote || escaped) return { error: "unterminated quote" };
  if (started) words.push(current);
  return { words };
}

function isSetupInvocation(value) {
  const parsed = consoleWords(value);
  if (!parsed.words || !parsed.words.length) return false;
  const name = parsed.words[0].replace(/^\//, "");
  return name === "setup";
}

function loadHistory() {
  try {
    // The original shell could store unmasked setup answers. Never import
    // those values into the corrected, versioned recall store.
    localStorage.removeItem(legacyHistoryKey);
  } catch (_) {
    // Disabled storage only removes durable recall; it must not stop the UI.
  }
  try {
    const saved = JSON.parse(localStorage.getItem(historyKey) || "[]");
    if (!Array.isArray(saved)) return [];
    return [...new Set(saved.filter(entry => typeof entry === "string" && entry.trim() && !isSetupInvocation(entry)))].slice(0, 50);
  } catch (_) {
    return [];
  }
}

function commandCandidates(value) {
  const query = firstWord(value).replace(/^\//, "").toLowerCase();
  const candidates = [];
  for (const command of commands) {
    const spellings = [command.name, ...(command.aliases || [])];
    for (const spelling of spellings) {
      const haystack = [spelling, command.name, command.description, command.category, command.webui].join(" ").toLowerCase();
      if (!query || haystack.includes(query)) {
        candidates.push({
          kind: "command",
          command,
          spelling,
          exact: spelling.toLowerCase() === query
        });
      }
    }
  }
  return candidates.sort((left, right) => Number(right.exact) - Number(left.exact));
}

function renderPalette(items) {
  displayed = items;
  const exact = items.findIndex(item => item.kind === "command" && item.exact);
  activeSuggestion = exact >= 0 ? exact : items.findIndex(item => item.kind !== "command" || item.command.webui === "supported");
  suggestions.replaceChildren();
  for (let index = 0; index < items.length; index++) {
    const item = items[index];
    const option = document.createElement("div");
    option.id = "suggestion-" + index;
    option.setAttribute("role", "option");
    const executable = item.kind !== "command" || item.command.webui === "supported";
    option.setAttribute("aria-disabled", String(!executable));
    option.setAttribute("aria-selected", String(index === activeSuggestion));
    option.className = "suggestion";
    if (item.kind === "path") {
      option.textContent = "@" + item.path;
    } else {
      const name = document.createElement("span");
      name.className = "suggestion-name";
      name.textContent = "/" + item.spelling;
      const meta = document.createElement("span");
      meta.className = "suggestion-meta";
      const aliases = (item.command.aliases || []).filter(alias => alias !== item.spelling);
      meta.textContent = [
        item.command.description,
        item.command.category,
        item.spelling !== item.command.name ? "alias for /" + item.command.name : "",
        aliases.length ? "aliases: " + aliases.join(", ") : "",
      ].filter(Boolean).join(" · ");
      const disposition = document.createElement("span");
      disposition.className = "suggestion-disposition";
      disposition.textContent = item.command.webui;
      option.append(name, meta, disposition);
    }
    option.addEventListener("mousedown", event => {
      event.preventDefault();
      chooseSuggestion(index);
    });
    suggestions.append(option);
  }
  suggestions.hidden = items.length === 0;
  line.setAttribute("aria-expanded", String(items.length > 0));
  updateActiveSuggestion();
}

function updateActiveSuggestion() {
  const options = suggestions.querySelectorAll('[role="option"]');
  options.forEach((option, index) => option.setAttribute("aria-selected", String(index === activeSuggestion)));
  if (activeSuggestion >= 0 && displayed[activeSuggestion]) {
    line.setAttribute("aria-activedescendant", "suggestion-" + activeSuggestion);
    options[activeSuggestion].scrollIntoView({ block: "nearest" });
  } else {
    line.removeAttribute("aria-activedescendant");
  }
}

function chooseSuggestion(index = activeSuggestion) {
  const item = displayed[index];
  if (!item) return false;
  if (item.kind === "command") {
    if (item.command.webui !== "supported") {
      setStatus("/" + item.spelling + " is " + item.command.webui + " and cannot be selected from discovery.");
      return false;
    }
    const context = commandCompletionContext();
    line.value = context.leading + "/" + item.spelling + (context.suffix || " ");
    const cursor = context.leading.length + item.spelling.length + 1;
    line.setSelectionRange(cursor, cursor);
  } else {
    line.value = line.value.slice(0, item.replaceStart) + item.path + line.value.slice(item.replaceEnd);
    const cursor = item.replaceStart + item.path.length;
    line.setSelectionRange(cursor, cursor);
  }
  closePalette();
  line.focus();
  return true;
}

function pathCompletionContext() {
  const cursor = line.selectionStart ?? line.value.length;
  const marker = line.value.lastIndexOf("@", cursor - 1);
  if (marker < 0) return null;
  const prefix = line.value.slice(marker + 1, cursor);
  if (/\s/.test(prefix)) return null;
  const tail = line.value.slice(cursor);
  const whitespace = tail.search(/[ \t]/);
  return {
    prefix,
    replaceStart: marker,
    replaceEnd: whitespace < 0 ? line.value.length : cursor + whitespace
  };
}

function moveSuggestion(direction) {
  if (!displayed.length) return false;
  activeSuggestion = (activeSuggestion + direction + displayed.length) % displayed.length;
  updateActiveSuggestion();
  return true;
}

function activeSuggestionExactlyMatchesTypedCommand() {
  const item = displayed[activeSuggestion];
  if (!item || item.kind !== "command" || !item.exact) return false;
  return item.spelling.toLowerCase() === commandCompletionContext().token.replace(/^\//, "").toLowerCase();
}

async function updateSuggestions() {
  // A setup response is not a command line. Keeping discovery closed for the
  // whole setup exchange prevents an ordinary answer from being inserted into
  // command recall or accidentally replaced with a command/path suggestion.
  if (setupActive) {
    closePalette();
    return;
  }
  const context = pathCompletionContext();
  if (context) {
    const generation = ++completionGeneration;
    try {
      const result = await request(apiRoutes.complete + "?prefix=" + encodeURIComponent(context.prefix));
      if (generation !== completionGeneration || (setupActive && setupMasked)) return;
      renderPalette((result.entries || []).map(entry => ({
        kind: "path",
        path: entry.path + (entry.dir ? "/" : ""),
        replaceStart: context.replaceStart,
        replaceEnd: context.replaceEnd
      })));
    } catch (_) {
      if (generation === completionGeneration) closePalette();
    }
    return;
  }
  completionGeneration++;
  renderPalette(commandCandidates(line.value));
}

function remember(typed) {
  history = [typed, ...history.filter(entry => entry !== typed)].slice(0, 50);
  try {
    localStorage.setItem(historyKey, JSON.stringify(history));
  } catch (_) {
    // Local recall is a convenience. A browser with disabled storage still has
    // the bounded in-memory history for this console session.
  }
}

function recallHistory() {
  if (!history.length) return false;
  if (historyIndex < 0) {
    historyDraft = line.value;
    historyIndex = 0;
  } else if (historyIndex === history.length - 1) {
    // Recall is Arrow-Up only. Cycling back to the value that was present
    // before recall gives a blank prompt its blank restoration without taking
    // Arrow-Down away from the native completion listbox.
    historyIndex = -1;
  } else {
    historyIndex++;
  }
  line.value = historyIndex < 0 ? historyDraft : history[historyIndex];
  closePalette();
  return true;
}

function renderSetupPrompt(prompt) {
  setupMasked = Boolean(prompt.masked) && !prompt.done;
  line.type = setupMasked ? "password" : "text";
  if (prompt.error) appendTranscript("Setup: " + prompt.error, "error");
  if (setupMasked) closePalette();
  if (prompt.text) appendTranscript(prompt.text);
  if (prompt.done) {
    setupActive = false;
    setupMasked = false;
    setStatus("Setup complete.");
    return;
  }
  const details = [];
  if (prompt.default) details.push("default: " + prompt.default);
  if (prompt.choices && prompt.choices.length) details.push("choices: " + prompt.choices.join(", "));
  if (details.length) appendTranscript("[" + details.join("; ") + "]");
  setStatus("Setup is waiting for input.");
}

function renderOutcome(outcome, wasCancelled = false) {
  if (wasCancelled || outcome.exit_code === 130) appendTranscript("Cancelled.");
  if (outcome.exit_code === 130) return;
  const messages = (outcome.messages || []).map(message => message.text).filter(Boolean);
  const renderedPayload = renderPayload(outcome.payload);
  if (renderedPayload) {
    if (messages.length && outcome.exit_code !== 0) {
      appendTranscript(messages.join("\n"), "error");
    }
    if (outcome.exit_code !== 0) {
      appendTranscript("Finished with exit code " + outcome.exit_code + ".", "error");
    }
    return;
  }
  if (messages.length) {
    appendTranscript(messages.join("\n"), outcome.exit_code === 0 ? "" : "error");
    return;
  }
  appendTranscript(outcome.exit_code === 0 ? "Completed." : "Finished with exit code " + (outcome.exit_code ?? "unknown") + ".");
}

function finish(id, outcome) {
  watchedJobs.delete(id);
  const wasCancelled = activeJob === id && cancelling;
  if (activeJob === id) {
    activeJob = null;
    cancelling = false;
    cancel.disabled = true;
  }
  renderOutcome(outcome || {}, wasCancelled);
  setStatus(wasCancelled ? "Command cancelled." : "Command finished.");
}

function watch(id) {
  if (watchedJobs.has(id)) return;
  watchedJobs.add(id);
  activeJob = id;
  cancelling = false;
  cancel.disabled = false;
  const events = new EventSource(jobEventsURL(id));
  events.addEventListener("progress", event => renderProgress(event.data));
  events.addEventListener("finished", event => {
    events.close();
    try {
      finish(id, JSON.parse(event.data));
    } catch (_) {
      request(jobURL(id)).then(job => {
        if (job.state === "finished") finish(id, job.outcome || {});
      }).catch(error => appendTranscript(error.message, "error"));
    }
  });
  events.onerror = () => {
    request(jobURL(id)).then(job => {
      if (job.state === "finished") {
        events.close();
        finish(id, job.outcome || {});
      }
    }).catch(error => appendTranscript(error.message, "error"));
  };
}

async function recoverJobs() {
  const jobs = await request(apiRoutes.jobs);
  for (const job of jobs) {
    if (job.state === "running") {
      appendTranscript("Reattached to " + job.command + ".");
      watch(job.id);
      continue;
    }
    const current = await request(jobURL(job.id));
    if (current.state === "finished") {
      appendTranscript("Recent " + job.command + ".");
      renderOutcome(current.outcome || {});
    }
  }
}

async function loadCommands() {
  const registered = await request(apiRoutes.commands);
  commands = registered;
  summary.textContent = "Commands: " + registered.length + " registered; /help for discovery.";
}

function localUsage(message) {
  appendTranscript(message, "error");
  setStatus("Command request failed.");
  return true;
}

function downloadTranscript() {
  const data = transcriptLines.join("\n") + (transcriptLines.length ? "\n" : "");
  const blob = new Blob([data], { type: "text/plain;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = "session.log";
  link.click();
  window.setTimeout(() => URL.revokeObjectURL(url), 0);
}

function sessionAPIKey(key) {
  // DMT calls this state-file; DMTX's existing authenticated session API calls
  // the same operative setting state. Keep both spellings ergonomic.
  return key === "state-file" ? "state" : key;
}

function displaySession(defaults) {
  const entries = Array.isArray(defaults) ? defaults.slice(0, 12) : [];
  const lines = ["Session defaults (/session KEY VALUE to set; /session clear [KEY] to unset):"];
  for (const entry of entries) {
    const item = payloadRecord(entry);
    if (!item) continue;
    const key = payloadText(item, "key");
    if (!key) continue;
    const value = payloadText(item, "value") || "(unset)";
    const description = payloadText(item, "description");
    lines.push("  " + (key === "state" ? "state-file" : key) + " = " + value + (description ? " — " + description : ""));
  }
  if (Array.isArray(defaults) && defaults.length > entries.length) lines.push("Additional defaults omitted.");
  appendTranscript(lines.join("\n"));
}

async function localSession(words) {
  if (words.length === 1) {
    const result = await request(apiRoutes.session);
    displaySession(result.defaults);
    setStatus("Session defaults shown.");
    return;
  }
  if (words[1] === "clear") {
    if (words.length > 3) throw new Error("usage: /session clear [KEY]");
    if (words.length === 3) {
      const key = sessionAPIKey(words[2]);
      await request(apiRoutes.session + "/" + encodeURIComponent(key), undefined, "DELETE");
      appendTranscript("Session default " + words[2] + " cleared.");
      setStatus("Session default cleared.");
      return;
    }
    const result = await request(apiRoutes.session);
    const defaults = Array.isArray(result.defaults) ? result.defaults : [];
    for (const entry of defaults) {
      const item = payloadRecord(entry);
      const key = payloadText(item, "key");
      if (key && payloadText(item, "value")) {
        await request(apiRoutes.session + "/" + encodeURIComponent(key), undefined, "DELETE");
      }
    }
    appendTranscript("All session defaults cleared.");
    setStatus("Session defaults cleared.");
    return;
  }
  if (words.length < 3) throw new Error("usage: /session [KEY VALUE] | /session clear [KEY]");
  const key = sessionAPIKey(words[1]);
  const value = words.slice(2).join(" ");
  await request(apiRoutes.session, { key, value });
  appendTranscript("Session default set: " + words[1] + " = " + value);
  setStatus("Session default set.");
}

async function localAbout() {
  const parsed = await request(apiRoutes.parse, { line: "version" });
  const version = ((parsed.outcome || {}).messages || []).map(message => payloadText(message, "text")).find(Boolean) || "unknown";
  appendTranscript("dmtx " + version + "\n\nDeterministic database migration tool.\n\nFeatures:\n- Parallel transfer with runtime safety tuning\n- Resume capability\n- Validation and durable history\n- Encrypted profiles\n- Guided setup");
  setStatus("About DMTX shown.");
}

async function localHelp() {
  const parsed = await request(apiRoutes.parse, { line: "help" });
  const messages = ((parsed.outcome || {}).messages || [])
    .map(message => payloadText(message, "text"))
    .filter(Boolean);
  if (messages.length) appendTranscript(messages.join("\n"));
  line.value = "";
  renderPalette(commandCandidates(""));
  setStatus("Command discovery is open.");
}

function setupInvocation(typed) {
  const parsed = consoleWords(typed);
  if (parsed.error) return { error: parsed.error };
  const words = parsed.words || [];
  if (!words.length) return null;
  const name = words[0].replace(/^\//, "");
  if (name !== "setup") return null;
  const usage = "usage: /setup [postgres] [CONFIG | @CONFIG | --config CONFIG | --profile NAME]";
  let index = 1;
  let engine = "sqlite";
  if (words[index] === "postgres") {
    engine = "postgres";
    index++;
  }
  let configPath = "";
  let profileName = "";
  while (index < words.length) {
    const argument = words[index];
    let value = "";
    if (argument === "--config") {
      value = words[index + 1] || "";
      index += 2;
      if (!value) return { error: "/setup: --config requires a path" };
      if (configPath || profileName) return { error: "/setup: choose one configuration path or profile" };
      configPath = value;
      continue;
    }
    if (argument.startsWith("--config=")) {
      value = argument.slice("--config=".length);
      index++;
      if (!value) return { error: "/setup: --config requires a path" };
      if (configPath || profileName) return { error: "/setup: choose one configuration path or profile" };
      configPath = value;
      continue;
    }
    if (argument === "--profile") {
      value = words[index + 1] || "";
      index += 2;
      if (!value) return { error: "/setup: --profile requires a name" };
      if (configPath || profileName) return { error: "/setup: choose one configuration path or profile" };
      profileName = value;
      continue;
    }
    if (argument.startsWith("--profile=")) {
      value = argument.slice("--profile=".length);
      index++;
      if (!value) return { error: "/setup: --profile requires a name" };
      if (configPath || profileName) return { error: "/setup: choose one configuration path or profile" };
      profileName = value;
      continue;
    }
    if (argument.startsWith("-")) return { error: "/setup: unknown flag " + argument + " (see /help)" };
    if (configPath || profileName) return { error: usage };
    configPath = argument;
    index++;
  }
  return {
    engine,
    config_path: configPath.replace(/^@/, ""),
    profile_name: profileName,
  };
}

async function localCommand(typed) {
  const parsed = consoleWords(typed);
  if (parsed.error) return localUsage(parsed.error);
  const words = parsed.words || [];
  if (!words.length) return false;
  const command = words[0].replace(/^\//, "");
  switch (command) {
    case "help":
      if (words.length !== 1) return localUsage("usage: /help");
      await localHelp();
      return true;
    case "clear":
      if (words.length !== 1) return localUsage("usage: /clear");
      clearTranscript();
      return true;
    case "quit":
    case "exit":
      if (words.length !== 1) return localUsage("usage: /quit");
      setStatus("Closing the console window.");
      window.close();
      window.setTimeout(() => setStatus("This tab remains open; close it manually when finished."), 100);
      return true;
    case "logs":
      if (words.length !== 1) return localUsage("usage: /logs");
      downloadTranscript();
      appendTranscript("Logs downloaded as session.log.");
      setStatus("Session log downloaded.");
      return true;
    case "session":
      await localSession(words);
      return true;
    case "about":
      if (words.length !== 1) return localUsage("usage: /about");
      await localAbout();
      return true;
    default:
      return false;
  }
}

line.addEventListener("focus", () => { updateSuggestions().catch(() => {}); });
line.addEventListener("input", () => {
  if (!setupActive || !setupMasked) historyIndex = -1;
  updateSuggestions().catch(() => {});
});
line.addEventListener("keydown", event => {
  if (event.isComposing) return;
  const paletteOpen = !suggestions.hidden;
  if (event.key === "Enter") {
    event.preventDefault();
    if (setupActive) {
      form.requestSubmit();
      return;
    }
    if (paletteOpen && activeSuggestion >= 0 && !activeSuggestionExactlyMatchesTypedCommand()) {
      if (chooseSuggestion()) return;
    }
    if (paletteOpen) closePalette();
    form.requestSubmit();
    return;
  }
  if (setupActive) return;
  if (event.key === "ArrowUp") {
    event.preventDefault();
    if (paletteOpen ? moveSuggestion(-1) : recallHistory()) return;
  }
  if (event.key === "ArrowDown") {
    if (paletteOpen) {
      event.preventDefault();
      moveSuggestion(1);
      return;
    }
  }
  if (event.key === "Tab" && paletteOpen && activeSuggestion >= 0) {
    if (chooseSuggestion()) event.preventDefault();
    return;
  }
  if (event.key === "Escape" && paletteOpen) {
    event.preventDefault();
    closePalette();
  }
});

cancel.addEventListener("click", () => {
  if (!activeJob || cancelling) return;
  cancelling = true;
  cancel.disabled = true;
  request(jobCancelURL(activeJob), {}).then(result => {
    if (result.state !== "cancelling") throw new Error("server did not accept cancellation");
    appendTranscript("Cancellation requested. Waiting for the command to stop safely.");
    setStatus("Cancellation requested.");
  }).catch(error => {
    cancelling = false;
    if (activeJob) cancel.disabled = false;
    appendTranscript(error.message, "error");
    setStatus("Cancellation request failed.");
  });
});

form.addEventListener("submit", async event => {
  event.preventDefault();
  const typed = setupActive && setupMasked ? line.value : line.value.trim();
  if (!typed) return;
  if (!setupActive) {
    if (!isSetupInvocation(typed)) remember(typed);
    line.value = "";
    historyDraft = "";
  }
  historyIndex = -1;
  closePalette();
  setStatus(setupActive ? "Sending setup response." : "Submitting command.");
  appendTranscript("> " + (setupActive && setupMasked ? "[REDACTED]" : typed), "command");
  try {
    if (setupActive) {
      const prompt = await request(apiRoutes.setupInput, { input: typed });
      renderSetupPrompt(prompt);
      line.value = "";
      return;
    }
    if (await localCommand(typed)) {
      line.value = "";
      return;
    }
    const setup = setupInvocation(typed);
    if (setup && setup.error) throw new Error(setup.error);
    if (setup) {
      const prompt = await request(apiRoutes.setupStart, setup);
      setupActive = true;
      renderSetupPrompt(prompt);
      line.value = "";
      return;
    }
    const parsed = await request(apiRoutes.parse, { line: typed });
    if (!parsed.dispatched) {
      appendTranscript((parsed.outcome.messages || []).map(message => message.text).join("\n"));
      setStatus("Command answered.");
      return;
    }
    const started = await request(apiRoutes.jobs, parsed.request);
    appendTranscript("Started " + started.command + ".");
    setStatus("Started " + started.command + ".");
    watch(started.id);
  } catch (error) {
    appendTranscript(error.message, "error");
    setStatus("Command request failed.");
  }
});

Promise.all([loadCommands(), recoverJobs()]).then(() => updateSuggestions()).catch(error => appendTranscript(error.message, "error"));
