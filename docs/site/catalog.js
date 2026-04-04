(function () {
  'use strict';

  var GROUP_ORDER = [
    'Core Commands',
    'Power Commands',
    'Computer Management',
    'Mobile Device Management',
    'Enrollment',
    'Inventory & Search',
    'Organization',
    'Users & Security',
    'Content & Configuration',
    'MDM & Certificates',
    'Server Administration',
    'Classic - Computers',
    'Classic - Mobile Devices',
    'Classic - Configuration',
    'Classic - Administration',
    'Classic - Patch Management',
    'Security Configuration',
    'Endpoints',
    'Access & Identity'
  ];

  var allCommands = [];
  var heroExamples = {};
  var activeProduct = 'all';
  var expandedCommand = null;

  // ===== Initialization =====

  function init() {
    loadHeroExamples();
    setupSearch();
    setupTabs();
    setupNavScroll();
    setupCopyButtons();
    fetchCommands();
  }

  function loadHeroExamples() {
    var el = document.getElementById('hero-examples');
    if (el) {
      try {
        heroExamples = JSON.parse(el.textContent);
      } catch (e) {
        heroExamples = {};
      }
    }
  }

  function fetchCommands() {
    fetch('commands.json')
      .then(function (res) {
        if (!res.ok) throw new Error('HTTP ' + res.status);
        return res.json();
      })
      .then(function (data) {
        allCommands = data.commands || [];
        populateStats(data);
        renderCatalog(allCommands, '', activeProduct);
        hideCatalogLoading();
      })
      .catch(function (err) {
        var catalog = document.getElementById('catalog');
        if (catalog) {
          catalog.innerHTML = '<p style="text-align:center;color:var(--text-secondary);padding:2rem 0;">Failed to load commands. ' +
            '<a href="https://github.com/Jamf-Concepts/jamf-cli">View on GitHub</a> instead.</p>';
        }
        hideCatalogLoading();
      });
  }

  function hideCatalogLoading() {
    var loading = document.getElementById('catalog-loading');
    if (loading) loading.style.display = 'none';
  }

  function populateStats(data) {
    var count = data.commandCount || data.commands.length;
    var version = data.version || '';
    var generatedAt = data.generatedAt || '';

    setText('command-count', count.toLocaleString());
    setText('stat-commands', count.toLocaleString());

    var versionBadge = document.getElementById('version-badge');
    if (versionBadge && version) {
      versionBadge.textContent = 'v' + version;
    }

    var lastUpdated = document.getElementById('last-updated');
    if (lastUpdated && generatedAt) {
      lastUpdated.textContent = 'Updated ' + formatDate(generatedAt);
    }

    var footerVersion = document.getElementById('footer-version');
    if (footerVersion && version) {
      footerVersion.textContent = ' · v' + version;
    }
  }

  function setText(id, text) {
    var el = document.getElementById(id);
    if (el) el.textContent = text;
  }

  function formatDate(iso) {
    try {
      var d = new Date(iso);
      return d.toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric' });
    } catch (e) {
      return iso;
    }
  }

  // ===== Rendering =====

  function renderCatalog(commands, searchQuery, productFilter) {
    var catalog = document.getElementById('catalog');
    if (!catalog) return;

    var filtered = filterCommands(commands, searchQuery, productFilter);
    var groups = groupCommands(filtered);
    var sorted = sortGroups(groups);

    catalog.innerHTML = '';

    if (sorted.length === 0) {
      catalog.innerHTML = '<p style="text-align:center;color:var(--text-secondary);padding:2rem 0;">No commands match your search.</p>';
      return;
    }

    for (var i = 0; i < sorted.length; i++) {
      catalog.appendChild(renderGroup(sorted[i]));
    }
  }

  function filterCommands(commands, query, product) {
    var results = commands;

    if (product && product !== 'all') {
      results = results.filter(function (cmd) {
        return cmd.product === product;
      });
    }

    if (query) {
      var q = query.toLowerCase();
      results = results.filter(function (cmd) {
        if (cmd.command.toLowerCase().indexOf(q) !== -1) return true;
        if (cmd.description && cmd.description.toLowerCase().indexOf(q) !== -1) return true;
        if (cmd.aliases) {
          for (var a = 0; a < cmd.aliases.length; a++) {
            if (cmd.aliases[a].toLowerCase().indexOf(q) !== -1) return true;
          }
        }
        if (cmd.flags) {
          for (var f = 0; f < cmd.flags.length; f++) {
            if (cmd.flags[f].toLowerCase().indexOf(q) !== -1) return true;
          }
        }
        return false;
      });
    }

    return results;
  }

  function groupCommands(commands) {
    var map = {};
    for (var i = 0; i < commands.length; i++) {
      var group = commands[i].group || 'Other';
      if (!map[group]) map[group] = [];
      map[group].push(commands[i]);
    }
    return map;
  }

  function sortGroups(groupMap) {
    var result = [];
    for (var i = 0; i < GROUP_ORDER.length; i++) {
      var name = GROUP_ORDER[i];
      if (groupMap[name]) {
        result.push({ name: name, commands: groupMap[name] });
        delete groupMap[name];
      }
    }
    // Append any groups not in the fixed order
    var remaining = Object.keys(groupMap).sort();
    for (var j = 0; j < remaining.length; j++) {
      result.push({ name: remaining[j], commands: groupMap[remaining[j]] });
    }
    return result;
  }

  function renderGroup(group) {
    var container = document.createElement('div');
    container.className = 'catalog-group';

    var header = document.createElement('div');
    header.className = 'catalog-group-header';
    header.setAttribute('role', 'button');
    header.setAttribute('aria-expanded', 'true');
    header.setAttribute('tabindex', '0');
    header.textContent = group.name + ' (' + group.commands.length + ')';

    var commandsDiv = document.createElement('div');
    commandsDiv.className = 'group-commands';

    header.addEventListener('click', function () {
      toggleGroup(header, commandsDiv);
    });
    header.addEventListener('keydown', function (e) {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        toggleGroup(header, commandsDiv);
      }
    });

    for (var i = 0; i < group.commands.length; i++) {
      var pair = renderCommandRow(group.commands[i]);
      commandsDiv.appendChild(pair.row);
      commandsDiv.appendChild(pair.detail);
    }

    container.appendChild(header);
    container.appendChild(commandsDiv);
    return container;
  }

  function toggleGroup(header, commandsDiv) {
    var expanded = header.getAttribute('aria-expanded') === 'true';
    header.setAttribute('aria-expanded', String(!expanded));
    commandsDiv.style.display = expanded ? 'none' : '';
  }

  function renderCommandRow(cmd) {
    var row = document.createElement('div');
    row.className = 'command-row';

    var nameSpan = document.createElement('span');
    nameSpan.className = 'command-name';
    nameSpan.textContent = cmd.command;
    row.appendChild(nameSpan);

    var descSpan = document.createElement('span');
    descSpan.className = 'command-desc';
    descSpan.textContent = cmd.description || '';
    row.appendChild(descSpan);

    var pillsWrap = document.createElement('span');
    pillsWrap.className = 'command-pills';

    if (cmd.aliases && cmd.aliases.length > 0) {
      for (var a = 0; a < cmd.aliases.length; a++) {
        var aliasBadge = document.createElement('span');
        aliasBadge.className = 'alias-badge';
        aliasBadge.textContent = cmd.aliases[a];
        pillsWrap.appendChild(aliasBadge);
      }
    }

    if (cmd.flags && cmd.flags.length > 0) {
      var maxPills = Math.min(cmd.flags.length, 5);
      for (var f = 0; f < maxPills; f++) {
        var pill = document.createElement('span');
        pill.className = 'flag-pill';
        pill.textContent = cmd.flags[f];
        pillsWrap.appendChild(pill);
      }
      if (cmd.flags.length > 5) {
        var more = document.createElement('span');
        more.className = 'flag-pill';
        more.textContent = '+' + (cmd.flags.length - 5);
        pillsWrap.appendChild(more);
      }
    }

    row.appendChild(pillsWrap);

    // Detail panel (hidden by default)
    var detail = document.createElement('div');
    detail.className = 'command-expanded';
    detail.appendChild(buildDetailContent(cmd));

    row.addEventListener('click', function () {
      var isOpen = detail.classList.contains('open');

      // Close any previously expanded command
      if (expandedCommand && expandedCommand !== detail) {
        expandedCommand.classList.remove('open');
      }

      detail.classList.toggle('open');
      expandedCommand = isOpen ? null : detail;
    });

    return { row: row, detail: detail };
  }

  function buildDetailContent(cmd) {
    var frag = document.createDocumentFragment();

    if (cmd.description) {
      var desc = document.createElement('p');
      desc.style.marginBottom = '0.5rem';
      desc.textContent = cmd.description;
      frag.appendChild(desc);
    }

    // All flags
    if (cmd.flags && cmd.flags.length > 0) {
      var flagsHeading = document.createElement('div');
      flagsHeading.style.fontSize = '0.75rem';
      flagsHeading.style.fontWeight = '600';
      flagsHeading.style.textTransform = 'uppercase';
      flagsHeading.style.letterSpacing = '0.05em';
      flagsHeading.style.color = 'var(--text-secondary)';
      flagsHeading.style.marginBottom = '0.35rem';
      flagsHeading.textContent = 'Flags';
      frag.appendChild(flagsHeading);

      var flagsWrap = document.createElement('div');
      flagsWrap.style.marginBottom = '0.5rem';
      for (var f = 0; f < cmd.flags.length; f++) {
        var pill = document.createElement('span');
        pill.className = 'flag-pill';
        pill.textContent = cmd.flags[f];
        flagsWrap.appendChild(pill);
      }
      frag.appendChild(flagsWrap);
    }

    // Aliases
    if (cmd.aliases && cmd.aliases.length > 0) {
      var aliasHeading = document.createElement('div');
      aliasHeading.style.fontSize = '0.75rem';
      aliasHeading.style.fontWeight = '600';
      aliasHeading.style.textTransform = 'uppercase';
      aliasHeading.style.letterSpacing = '0.05em';
      aliasHeading.style.color = 'var(--text-secondary)';
      aliasHeading.style.marginBottom = '0.35rem';
      aliasHeading.textContent = 'Aliases';
      frag.appendChild(aliasHeading);

      var aliasWrap = document.createElement('div');
      aliasWrap.style.marginBottom = '0.5rem';
      for (var a = 0; a < cmd.aliases.length; a++) {
        var badge = document.createElement('span');
        badge.className = 'alias-badge';
        badge.textContent = cmd.aliases[a];
        aliasWrap.appendChild(badge);
      }
      frag.appendChild(aliasWrap);
    }

    // Hero example (if available)
    var heroKey = findHeroKey(cmd.command);
    if (heroKey) {
      var example = heroExamples[heroKey];
      var pre = document.createElement('pre');
      var promptLine = '$ ' + example.example + '\n';
      pre.textContent = promptLine + example.output;
      frag.appendChild(pre);
    }

    return frag;
  }

  function findHeroKey(command) {
    // Strip the "jamf-cli " prefix if present in the command path,
    // then try matching against hero example keys.
    // Hero keys are like "pro overview", "protect analytics list"
    // Command paths from commands.json are like "pro overview", "pro computers list"
    var keys = Object.keys(heroExamples);
    for (var i = 0; i < keys.length; i++) {
      if (command === keys[i]) return keys[i];
      // Also try matching the command path without its leaf subcommand
      // e.g., "pro computers list" matches hero key "pro computers list"
      // and "pro comp erase" matches hero key "pro comp erase"
    }
    return null;
  }

  // ===== Search =====

  function setupSearch() {
    var search = document.getElementById('search');
    if (!search) return;

    search.addEventListener('input', function () {
      var query = search.value.trim();
      renderCatalog(allCommands, query, activeProduct);
    });
  }

  // ===== Product Tabs =====

  function setupTabs() {
    var tabs = document.querySelectorAll('.tab');
    for (var i = 0; i < tabs.length; i++) {
      tabs[i].addEventListener('click', function () {
        for (var j = 0; j < tabs.length; j++) {
          tabs[j].classList.remove('active');
        }
        this.classList.add('active');
        activeProduct = this.getAttribute('data-filter');
        var search = document.getElementById('search');
        var query = search ? search.value.trim() : '';
        renderCatalog(allCommands, query, activeProduct);
      });
    }
  }

  // ===== Nav Scroll (IntersectionObserver) =====

  function setupNavScroll() {
    var hero = document.getElementById('hero');
    var nav = document.querySelector('nav');
    if (!hero || !nav || !('IntersectionObserver' in window)) return;

    var observer = new IntersectionObserver(function (entries) {
      if (entries[0].isIntersecting) {
        nav.classList.remove('scrolled');
      } else {
        nav.classList.add('scrolled');
      }
    }, { threshold: 0 });

    observer.observe(hero);
  }

  // ===== Copy Buttons =====

  function setupCopyButtons() {
    // Delegate copy handling for any .copy-btn, including dynamically added ones.
    // Static copy buttons in index.html already have their own listeners,
    // so we use event delegation on #catalog for catalog-scoped buttons only.
    var catalog = document.getElementById('catalog');
    if (!catalog) return;
    catalog.addEventListener('click', function (e) {
      var btn = e.target.closest('.copy-btn');
      if (!btn) return;
      e.stopPropagation();
      var text = btn.getAttribute('data-copy');
      if (!text) return;
      navigator.clipboard.writeText(text).then(function () {
        btn.classList.add('copied');
        setTimeout(function () { btn.classList.remove('copied'); }, 2000);
      });
    });
  }

  // ===== Bootstrap =====

  document.addEventListener('DOMContentLoaded', init);
})();
