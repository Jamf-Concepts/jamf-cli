// Cmd+K command palette — Linear/Raycast-style fuzzy search across every
// jamf-cli command. Loads commands.json on demand, scores by substring +
// word-boundary bonuses, jumps to the catalog with the chosen command
// pre-filled in the search input. Pure DOM (no innerHTML) for safety.
(function () {
  'use strict';

  var RECENT_KEY = 'palette-recent';
  var MAX_RECENT = 5;
  var MAX_RESULTS = 60;

  var commands = [];
  var loaded = false;
  var isOpen = false;
  var selectedIdx = 0;
  var filtered = [];

  var modal, input, list, preview, hint;

  function init() {
    buildDOM();
    document.addEventListener('keydown', onGlobalKey);
    fetch('commands.json')
      .then(function (r) { return r.ok ? r.json() : null; })
      .then(function (data) {
        if (data && data.commands) {
          commands = data.commands;
          loaded = true;
          if (input && input.placeholder) {
            input.placeholder = 'Search ' + thousands(commands.length) + ' commands…';
          }
          if (isOpen) render();
        }
      })
      .catch(function () { /* silent — palette stays in "loading" mode */ });
  }

  function buildDOM() {
    modal = document.createElement('div');
    modal.className = 'cmd-palette';
    modal.setAttribute('aria-hidden', 'true');

    var backdrop = document.createElement('div');
    backdrop.className = 'palette-backdrop';
    backdrop.addEventListener('click', close);
    modal.appendChild(backdrop);

    var win = document.createElement('div');
    win.className = 'palette-window';
    win.setAttribute('role', 'dialog');
    win.setAttribute('aria-label', 'Command palette');

    // Search row
    var search = document.createElement('div');
    search.className = 'palette-search';
    var prompt = document.createElement('span');
    prompt.className = 'palette-prompt';
    prompt.textContent = '$';
    input = document.createElement('input');
    input.type = 'text';
    input.className = 'palette-input';
    input.placeholder = 'Search commands…';
    input.setAttribute('autocomplete', 'off');
    input.setAttribute('spellcheck', 'false');
    input.setAttribute('aria-label', 'Search commands');
    input.addEventListener('input', render);
    input.addEventListener('keydown', onInputKey);
    var escKbd = document.createElement('kbd');
    escKbd.className = 'palette-kbd';
    escKbd.textContent = 'esc';
    search.appendChild(prompt);
    search.appendChild(input);
    search.appendChild(escKbd);
    win.appendChild(search);

    // Body: list + preview
    var body = document.createElement('div');
    body.className = 'palette-body';
    list = document.createElement('ul');
    list.className = 'palette-list';
    list.setAttribute('role', 'listbox');
    preview = document.createElement('div');
    preview.className = 'palette-preview';
    body.appendChild(list);
    body.appendChild(preview);
    win.appendChild(body);

    // Footer hints
    hint = document.createElement('div');
    hint.className = 'palette-footer';
    var hints = [
      ['↑↓', 'navigate'],
      ['↵', 'jump'],
      ['⌘C', 'copy'],
      ['esc', 'close'],
    ];
    hints.forEach(function (h) {
      var k = document.createElement('kbd');
      k.textContent = h[0];
      var lbl = document.createElement('span');
      lbl.className = 'palette-footer-lbl';
      lbl.textContent = h[1];
      hint.appendChild(k);
      hint.appendChild(lbl);
    });
    win.appendChild(hint);

    modal.appendChild(win);
    document.body.appendChild(modal);
  }

  function onGlobalKey(e) {
    // Cmd/Ctrl + K — toggle
    if ((e.metaKey || e.ctrlKey) && (e.key === 'k' || e.key === 'K')) {
      e.preventDefault();
      isOpen ? close() : open();
      return;
    }
    if (!isOpen) return;
    if (e.key === 'Escape') {
      e.preventDefault();
      close();
    }
  }

  function onInputKey(e) {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      selectedIdx = Math.min(selectedIdx + 1, filtered.length - 1);
      paintList();
      paintPreview();
      scrollSelectedIntoView();
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      selectedIdx = Math.max(selectedIdx - 1, 0);
      paintList();
      paintPreview();
      scrollSelectedIntoView();
    } else if (e.key === 'Enter') {
      e.preventDefault();
      jumpTo();
    } else if ((e.metaKey || e.ctrlKey) && (e.key === 'c' || e.key === 'C')) {
      // Only intercept when nothing is selected in the input — let normal
      // copy work if the user has highlighted text.
      if (input.selectionStart === input.selectionEnd && filtered[selectedIdx]) {
        e.preventDefault();
        copyCommand(filtered[selectedIdx]);
      }
    }
  }

  function open() {
    isOpen = true;
    modal.classList.add('open');
    modal.setAttribute('aria-hidden', 'false');
    input.value = '';
    selectedIdx = 0;
    render();
    setTimeout(function () { input.focus(); }, 0);
  }

  function close() {
    isOpen = false;
    modal.classList.remove('open');
    modal.setAttribute('aria-hidden', 'true');
  }

  function render() {
    var q = input.value.trim();

    if (!loaded) {
      filtered = [];
      paintEmpty('Loading commands…');
      preview.replaceChildren();
      return;
    }

    if (!q) {
      // Default view: recent searches first, then top of catalog
      var recent = readRecent();
      var pinned = [];
      var seen = {};
      recent.forEach(function (cmdStr) {
        var c = byName(cmdStr);
        if (c && !seen[c.command]) {
          pinned.push(c);
          seen[c.command] = true;
        }
      });
      // Fill out with first commands from catalog
      for (var i = 0; i < commands.length && pinned.length < 30; i++) {
        if (!seen[commands[i].command]) {
          pinned.push(commands[i]);
          seen[commands[i].command] = true;
        }
      }
      filtered = pinned;
    } else {
      var ql = q.toLowerCase();
      var scored = [];
      for (var j = 0; j < commands.length; j++) {
        var s = score(ql, commands[j]);
        if (s !== null) scored.push({ c: commands[j], s: s });
      }
      scored.sort(function (a, b) { return a.s - b.s; });
      filtered = scored.slice(0, MAX_RESULTS).map(function (e) { return e.c; });
    }

    selectedIdx = 0;
    paintList();
    paintPreview();
  }

  function score(q, cmd) {
    // Lower score = better. Returns null if no match.
    var name = cmd.command.toLowerCase();
    var idx = name.indexOf(q);
    if (idx >= 0) {
      var s = idx;
      if (idx === 0) s -= 200;
      else if (name[idx - 1] === ' ') s -= 100;
      else if (name[idx - 1] === '-') s -= 50;
      return s;
    }
    // Fallback: description match (lower priority)
    if (cmd.description && cmd.description.toLowerCase().indexOf(q) >= 0) {
      return 1000;
    }
    return null;
  }

  function byName(s) {
    for (var i = 0; i < commands.length; i++) {
      if (commands[i].command === s) return commands[i];
    }
    return null;
  }

  function readRecent() {
    try {
      return JSON.parse(localStorage.getItem(RECENT_KEY) || '[]');
    } catch (e) { return []; }
  }
  function writeRecent(name) {
    var prev = readRecent().filter(function (n) { return n !== name; });
    prev.unshift(name);
    try { localStorage.setItem(RECENT_KEY, JSON.stringify(prev.slice(0, MAX_RECENT))); }
    catch (e) { /* localStorage might be unavailable */ }
  }

  function paintEmpty(msg) {
    list.replaceChildren();
    var li = document.createElement('li');
    li.className = 'palette-empty';
    li.textContent = msg;
    list.appendChild(li);
  }

  function paintList() {
    list.replaceChildren();
    if (filtered.length === 0) {
      paintEmpty('No matches');
      return;
    }
    var q = input.value.trim().toLowerCase();
    filtered.forEach(function (c, i) {
      var li = document.createElement('li');
      li.className = 'palette-item' + (i === selectedIdx ? ' selected' : '');
      li.setAttribute('role', 'option');
      li.setAttribute('aria-selected', i === selectedIdx ? 'true' : 'false');

      var prefix = document.createElement('span');
      prefix.className = 'palette-item-prefix';
      prefix.textContent = 'jamf-cli';
      li.appendChild(prefix);

      var name = document.createElement('span');
      name.className = 'palette-item-name';
      // Highlight matched substring
      if (q && c.command.toLowerCase().indexOf(q) >= 0) {
        var lower = c.command.toLowerCase();
        var idx = lower.indexOf(q);
        if (idx > 0) name.appendChild(document.createTextNode(c.command.slice(0, idx)));
        var hit = document.createElement('mark');
        hit.textContent = c.command.slice(idx, idx + q.length);
        name.appendChild(hit);
        if (idx + q.length < c.command.length) {
          name.appendChild(document.createTextNode(c.command.slice(idx + q.length)));
        }
      } else {
        name.textContent = c.command;
      }
      li.appendChild(name);

      if (c.product) {
        var tag = document.createElement('span');
        tag.className = 'palette-item-tag';
        tag.setAttribute('data-product', c.product);
        tag.textContent = c.product;
        li.appendChild(tag);
      }

      li.addEventListener('mouseenter', function () {
        if (selectedIdx !== i) {
          selectedIdx = i;
          paintList();
          paintPreview();
        }
      });
      li.addEventListener('click', function () {
        selectedIdx = i;
        jumpTo();
      });

      list.appendChild(li);
    });
  }

  function paintPreview() {
    preview.replaceChildren();
    var c = filtered[selectedIdx];
    if (!c) return;

    var head = document.createElement('div');
    head.className = 'palette-cmd';
    var dol = document.createElement('span');
    dol.className = 'palette-cmd-dollar';
    dol.textContent = '$ ';
    head.appendChild(dol);
    head.appendChild(document.createTextNode('jamf-cli ' + c.command));
    preview.appendChild(head);

    if (c.description) {
      var desc = document.createElement('div');
      desc.className = 'palette-desc';
      desc.textContent = c.description;
      preview.appendChild(desc);
    }

    if (c.product || c.group) {
      var meta = document.createElement('div');
      meta.className = 'palette-meta';
      if (c.product) {
        var p = document.createElement('span');
        p.className = 'palette-tag';
        p.setAttribute('data-product', c.product);
        p.textContent = labelFor(c.product);
        meta.appendChild(p);
      }
      if (c.group) {
        var g = document.createElement('span');
        g.className = 'palette-group';
        g.textContent = c.group;
        meta.appendChild(g);
      }
      preview.appendChild(meta);
    }

    if (c.flags && c.flags.length) {
      var fhead = document.createElement('div');
      fhead.className = 'palette-section';
      fhead.textContent = 'Flags';
      preview.appendChild(fhead);
      var flist = document.createElement('div');
      flist.className = 'palette-flags';
      c.flags.forEach(function (f) {
        var fl = document.createElement('code');
        fl.textContent = f;
        flist.appendChild(fl);
      });
      preview.appendChild(flist);
    }

    if (c.aliases && c.aliases.length) {
      var ahead = document.createElement('div');
      ahead.className = 'palette-section';
      ahead.textContent = 'Aliases';
      preview.appendChild(ahead);
      var aliases = document.createElement('div');
      aliases.className = 'palette-aliases';
      c.aliases.forEach(function (a) {
        var ac = document.createElement('code');
        ac.textContent = a;
        aliases.appendChild(ac);
      });
      preview.appendChild(aliases);
    }
  }

  function labelFor(p) {
    return ({ pro: 'Jamf Pro', protect: 'Jamf Protect', school: 'Jamf School', platform: 'Jamf Platform' })[p] || p;
  }

  function thousands(n) {
    return String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ',');
  }

  function scrollSelectedIntoView() {
    var el = list.children[selectedIdx];
    if (el && el.scrollIntoView) el.scrollIntoView({ block: 'nearest' });
  }

  function jumpTo() {
    var c = filtered[selectedIdx];
    if (!c) return;
    writeRecent(c.command);
    close();

    // Pipe the chosen command into the catalog search and scroll there.
    var search = document.getElementById('search');
    if (search) {
      search.value = c.command;
      search.dispatchEvent(new Event('input', { bubbles: true }));
      var wrap = search.closest('.search-wrap');
      if (wrap) wrap.classList.add('has-value');
    }
    var anchor = document.getElementById('commands');
    if (anchor) anchor.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }

  function copyCommand(c) {
    var txt = 'jamf-cli ' + c.command;
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(txt).then(flashCopied).catch(function () {});
    }
  }
  function flashCopied() {
    if (!hint) return;
    hint.classList.add('copied');
    setTimeout(function () { hint.classList.remove('copied'); }, 900);
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
