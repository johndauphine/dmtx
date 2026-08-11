"use strict";

// Kept as data so the Go asset test can ask the real mux about each static
// route the browser uses. Dynamic job URLs are expressed by the functions
// below and covered by synthesised job ids in that same test.
const apiRoutes = Object.freeze({
  commands: "/api/v1/commands",
  parse: "/api/v1/parse",
  complete: "/api/v1/complete",
  jobs: "/api/v1/jobs",
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

const shellCommands = [
  { name: "help", aliases: [], description: "Show command discovery", category: "shell", webui: "supported", local: true },
  { name: "clear", aliases: [], description: "Clear the transcript", category: "shell", webui: "supported", local: true },
  { name: "quit", aliases: [], description: "Close this console window", category: "shell", webui: "supported", local: true }
];

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

function jobURL(id) { return apiRoutes.jobs + "/" + encodeURIComponent(id); }
function jobEventsURL(id) { return jobURL(id) + "/events"; }
function jobCancelURL(id) { return jobURL(id) + "/cancel"; }

function appendTranscript(value, kind = "") {
  const entry = document.createElement("pre");
  entry.className = "transcript-line" + (kind ? " transcript-" + kind : "");
  entry.textContent = String(value);
  transcript.append(entry);
}

function setStatus(value) { status.textContent = String(value); }

function clearTranscript() {
  transcript.replaceChildren();
  setStatus("Transcript cleared.");
}

async function request(path, body) {
  const response = await fetch(path, {
    method: body === undefined ? "GET" : "POST",
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

function isSetupInvocation(value) {
  const words = value.trim().replace(/^\//, "").split(/[ \t]+/);
  return words[0] === "setup";
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
  const messages = (outcome.messages || []).map(message => message.text).filter(Boolean);
  if (messages.length) {
    appendTranscript(messages.join("\n"), outcome.exit_code === 0 ? "" : "error");
    return;
  }
  if (outcome.exit_code === 130) return;
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
  events.addEventListener("progress", event => appendTranscript(event.data));
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
  commands = [...registered, ...shellCommands];
  summary.textContent = "Commands: " + registered.length + " registered; /help for discovery.";
}

function localCommand(typed) {
  const words = typed.replace(/^\//, "").split(/[ \t]+/);
  if (words.length !== 1) return false;
  switch (words[0]) {
    case "help":
      line.value = "";
      renderPalette(commandCandidates(""));
      setStatus("Command discovery is open.");
      return true;
    case "clear":
      clearTranscript();
      return true;
    case "quit":
      setStatus("Closing the console window.");
      window.close();
      window.setTimeout(() => setStatus("This tab remains open; close it manually when finished."), 100);
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
    if (localCommand(typed)) {
      line.value = "";
      return;
    }
    const setupWords = typed.replace(/^\//, "");
    if (setupWords === "setup" || setupWords === "setup postgres") {
      const prompt = await request(apiRoutes.setupStart, { engine: setupWords === "setup postgres" ? "postgres" : "sqlite" });
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
