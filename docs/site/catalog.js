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

  var JAMF_ICON_SVG = '<svg class="product-icon" viewBox="0 0 43 43" fill="none" xmlns="http://www.w3.org/2000/svg"><path fill-rule="evenodd" clip-rule="evenodd" d="M33.63 6.96c-5.52 0-8.78 2.33-10.27 7.31l-3.84 11.88c-1.41 3.91-3.68 5.52-7.82 5.52H2.19A2.19 2.19 0 000 33.87v6.06c0 1.16.94 2.1 2.1 2.1h38.57a2.19 2.19 0 002.19-2.19V9.08c0-1.17-.95-2.12-2.11-2.12h-7.12z" fill="currentColor"/><path fill-rule="evenodd" clip-rule="evenodd" d="M2.35 0A2.35 2.35 0 000 2.35v16.22a2.29 2.29 0 002.29 2.29h8.17c3.74 0 8.88-.77 10.29-7.41l2.23-10.6A2.35 2.35 0 0020.68 0H2.35z" fill="currentColor"/></svg>';

  var allCommands = [];
  var heroExamples = {};
  var activeProduct = 'all';
  var expandedCommand = null;

  // ===== Initialization =====

  function init() {
    loadHeroExamples();
    setupSearch();
    setupTabs();
    setupStatCards();
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

    var proCount = 0;
    var protectCount = 0;
    for (var i = 0; i < data.commands.length; i++) {
      if (data.commands[i].product === 'pro') proCount++;
      else if (data.commands[i].product === 'protect') protectCount++;
    }
    setText('stat-pro', proCount.toLocaleString());
    setText('stat-protect', protectCount.toLocaleString());

    // Count unique top-level resources (second path segment, e.g. "pro computers list" → "computers")
    var resources = {};
    for (var r = 0; r < data.commands.length; r++) {
      var parts = data.commands[r].command.split(' ');
      if (parts.length >= 2) {
        var key = parts.slice(0, 2).join(' ');
        resources[key] = true;
      }
    }
    setText('stat-resources', Object.keys(resources).length.toLocaleString());

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
    if (el) {
      el.textContent = text;
      el.classList.add('loaded');
    }
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

    var hasSearch = !!(searchQuery && searchQuery.trim());
    var lastProduct = '';
    for (var i = 0; i < sorted.length; i++) {
      var groupProduct = sorted[i].commands.length > 0 ? (sorted[i].commands[0].product || '') : '';
      if (groupProduct && groupProduct !== lastProduct) {
        catalog.appendChild(renderProductDivider(groupProduct));
        lastProduct = groupProduct;
      }
      catalog.appendChild(renderGroup(sorted[i], hasSearch));
    }
  }

  var PRODUCT_LABELS = {
    pro: 'Jamf Pro',
    protect: 'Jamf Protect'
    // school: 'Jamf School',
    // connect: 'Jamf Connect'
  };

  function renderProductDivider(product) {
    var divider = document.createElement('div');
    divider.className = 'product-divider';
    divider.setAttribute('data-product', product);

    var label = document.createElement('span');
    label.className = 'product-divider-label';
    label.textContent = PRODUCT_LABELS[product] || product;
    divider.appendChild(label);

    return divider;
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

  function renderGroup(group, startExpanded) {
    var container = document.createElement('div');
    container.className = 'catalog-group';

    // Determine which products are in this group
    var products = {};
    for (var p = 0; p < group.commands.length; p++) {
      var prod = group.commands[p].product;
      if (prod) products[prod] = true;
    }
    var productKeys = Object.keys(products);
    if (productKeys.length === 1) {
      container.setAttribute('data-product', productKeys[0]);
    }

    var header = document.createElement('div');
    header.className = 'catalog-group-header';
    header.setAttribute('role', 'button');
    var expanded = !!startExpanded;
    header.setAttribute('aria-expanded', String(expanded));
    header.setAttribute('tabindex', '0');

    var chevron = document.createElement('span');
    chevron.className = 'group-chevron';
    chevron.textContent = '\u25B6';
    header.appendChild(chevron);

    var headerText = document.createTextNode(' ' + group.name + ' ');
    header.appendChild(headerText);

    var countBadge = document.createElement('span');
    countBadge.className = 'group-count-badge';
    countBadge.textContent = group.commands.length;
    header.appendChild(countBadge);

    // Show product badges if group has commands from multiple products, or for Core/shared groups
    if (productKeys.length > 1 || (productKeys.length === 1 && group.name === 'Core Commands')) {
      for (var b = 0; b < productKeys.length; b++) {
        var badge = document.createElement('span');
        badge.className = 'product-badge';
        badge.setAttribute('data-product', productKeys[b]);
        badge.innerHTML = JAMF_ICON_SVG + ' ' + (PRODUCT_LABELS[productKeys[b]] || productKeys[b]);
        header.appendChild(badge);
      }
    }

    var commandsDiv = document.createElement('div');
    commandsDiv.className = 'group-commands';
    commandsDiv.style.display = expanded ? '' : 'none';

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
    if (cmd.product) {
      row.setAttribute('data-product', cmd.product);
    }

    var nameSpan = document.createElement('span');
    nameSpan.className = 'command-name';
    // Split command into prefix (dimmed) and action (bold)
    var parts = cmd.command.split(' ');
    if (parts.length > 1) {
      var prefix = document.createElement('span');
      prefix.className = 'cmd-prefix';
      prefix.textContent = parts.slice(0, -1).join(' ') + ' ';
      nameSpan.appendChild(prefix);
      var action = document.createElement('span');
      action.className = 'cmd-action';
      action.textContent = parts[parts.length - 1];
      nameSpan.appendChild(action);
    } else {
      nameSpan.textContent = cmd.command;
    }
    row.appendChild(nameSpan);

    var descSpan = document.createElement('span');
    descSpan.className = 'command-desc';
    descSpan.textContent = cmd.description || '';
    row.appendChild(descSpan);

    var hasAliases = cmd.aliases && cmd.aliases.length > 0;
    var hasFlags = cmd.flags && cmd.flags.length > 0;

    if (hasAliases || hasFlags) {
      var meta = document.createElement('div');
      meta.className = 'command-meta';

      if (hasAliases) {
        for (var a = 0; a < cmd.aliases.length; a++) {
          var aliasBadge = document.createElement('span');
          aliasBadge.className = 'alias-badge';
          aliasBadge.textContent = cmd.aliases[a];
          meta.appendChild(aliasBadge);
        }
      }

      if (hasFlags) {
        for (var f = 0; f < cmd.flags.length; f++) {
          var pill = document.createElement('span');
          pill.className = 'flag-pill';
          pill.textContent = cmd.flags[f];
          meta.appendChild(pill);
        }
      }

      row.appendChild(meta);
    }

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

  function setupStatCards() {
    var cards = document.querySelectorAll('.stat-card[data-tab]');
    for (var i = 0; i < cards.length; i++) {
      cards[i].addEventListener('click', function (e) {
        var tabFilter = this.getAttribute('data-tab');
        var tabs = document.querySelectorAll('.tab');
        for (var j = 0; j < tabs.length; j++) {
          tabs[j].classList.remove('active');
          if (tabs[j].getAttribute('data-filter') === tabFilter) {
            tabs[j].classList.add('active');
          }
        }
        activeProduct = tabFilter;
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
