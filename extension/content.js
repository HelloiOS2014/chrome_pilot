'use strict';

/**
 * content.js — Core content script for chrome-pilot.
 *
 * Injected into web pages via chrome.scripting.executeScript.
 * Export: executeInPage(method, params) → { result } | { error }
 *
 * Handles:
 *   snapshot        — accessibility-tree snapshot with data-cp-ref assignment
 *   dom.click       — click / dblclick by ref
 *   dom.type        — type text (supports --slowly, --submit)
 *   dom.hover       — mouseenter + mouseover
 *   dom.key         — KeyboardEvent on activeElement
 *   dom.select      — <select> option by value or label
 *   dom.drag        — HTML5 drag-and-drop
 *   dom.eval        — run arbitrary JS with optional element context
 *   page.content    — return document HTML / text
 *   page.wait       — poll for text presence / absence (async)
 */

// ─── Visibility / interactability helpers ─────────────────────────────────

const INTERACTABLE_ROLES = new Set([
  'button', 'link', 'textbox', 'checkbox', 'radio', 'combobox', 'slider',
  'switch', 'tab', 'menuitem', 'option', 'searchbox', 'spinbutton', 'treeitem',
]);

/** True if the element is visible and not aria-hidden. */
function isVisible(el) {
  if (!el || el.nodeType !== Node.ELEMENT_NODE) return false;
  const style = window.getComputedStyle(el);
  if (style.display === 'none') return false;
  if (style.visibility === 'hidden') return false;
  if (style.opacity === '0') return false;
  if (el.getAttribute('aria-hidden') === 'true') return false;
  // Zero-size elements are invisible unless they have overflow-visible children
  const rect = el.getBoundingClientRect();
  if (rect.width === 0 && rect.height === 0) {
    // Allow if it has visible children (e.g. icons in a 0×0 wrapper)
    if (!el.children.length) return false;
  }
  return true;
}

// ─── Role / name derivation ────────────────────────────────────────────────

const TAG_ROLE_MAP = {
  A: 'link',
  BUTTON: 'button',
  TEXTAREA: 'textbox',
  SELECT: 'combobox',
  H1: 'heading', H2: 'heading', H3: 'heading',
  H4: 'heading', H5: 'heading', H6: 'heading',
  NAV: 'navigation',
  MAIN: 'main',
  ASIDE: 'complementary',
  HEADER: 'banner',
  FOOTER: 'contentinfo',
  FORM: 'form',
  TABLE: 'table',
  IMG: 'img',
  DIALOG: 'dialog',
  LI: 'listitem',
  UL: 'list',
  OL: 'list',
  ARTICLE: 'article',
  SECTION: 'region',
};

/** Derive ARIA role for an element. */
function getRole(el) {
  // Explicit ARIA role always wins
  const ariaRole = el.getAttribute('role');
  if (ariaRole) return ariaRole.trim().split(/\s+/)[0];

  const tag = el.tagName;

  // INPUT variants
  if (tag === 'INPUT') {
    const type = (el.type || 'text').toLowerCase();
    if (type === 'checkbox') return 'checkbox';
    if (type === 'radio') return 'radio';
    if (type === 'submit' || type === 'button' || type === 'reset') return 'button';
    if (type === 'range') return 'slider';
    if (type === 'search') return 'searchbox';
    if (type === 'number') return 'spinbutton';
    return 'textbox';
  }

  if (TAG_ROLE_MAP[tag]) return TAG_ROLE_MAP[tag];

  // Elements with cursor:pointer behave as interactive
  const style = window.getComputedStyle(el);
  if (style.cursor === 'pointer') return 'button';

  return null; // no semantic role
}

/** Derive accessible name for an element. */
function getAccessibleName(el) {
  // aria-label
  const ariaLabel = el.getAttribute('aria-label');
  if (ariaLabel?.trim()) return ariaLabel.trim();

  // aria-labelledby
  const labelledBy = el.getAttribute('aria-labelledby');
  if (labelledBy) {
    const parts = labelledBy.split(/\s+/)
      .map(id => document.getElementById(id)?.textContent?.trim())
      .filter(Boolean);
    if (parts.length) return parts.join(' ');
  }

  // alt (images)
  if (el.tagName === 'IMG') {
    const alt = el.getAttribute('alt');
    if (alt !== null) return alt.trim(); // empty alt is intentional
  }

  // placeholder
  const placeholder = el.getAttribute('placeholder');
  if (placeholder?.trim()) return placeholder.trim();

  // <label> associated via for= or wrapping
  if (el.id) {
    const label = document.querySelector(`label[for="${CSS.escape(el.id)}"]`);
    if (label) return label.textContent.trim();
  }

  // title attribute
  const title = el.getAttribute('title');
  if (title?.trim()) return title.trim();

  // Collapse text content (reasonable limit)
  const text = el.textContent?.trim();
  if (text) return text.slice(0, 200);

  return '';
}

// ─── Snapshot ─────────────────────────────────────────────────────────────

/** Walk the DOM and build an accessibility tree, assigning data-cp-ref refs. */
function buildSnapshot() {
  let refCounter = 0;

  /**
   * Recursively build a node object.
   * Returns null if element should be fully skipped.
   */
  function walk(el) {
    if (el.nodeType !== Node.ELEMENT_NODE) return null;

    // Skip invisible / aria-hidden elements
    if (!isVisible(el)) return null;

    // Skip script/style/meta/noscript
    const tag = el.tagName;
    if (['SCRIPT', 'STYLE', 'META', 'NOSCRIPT', 'LINK', 'HEAD'].includes(tag)) return null;

    const role = getRole(el);
    const name = getAccessibleName(el);
    const isInteractable = INTERACTABLE_ROLES.has(role);

    // Assign ref to interactable elements
    let ref = null;
    if (isInteractable) {
      refCounter += 1;
      ref = `e${refCounter}`;
      el.setAttribute('data-cp-ref', ref);
    }

    // Walk children
    const childNodes = Array.from(el.childNodes);
    const children = [];
    for (const child of childNodes) {
      if (child.nodeType === Node.TEXT_NODE) {
        const text = child.textContent?.trim();
        if (text) children.push({ type: 'text', content: text.slice(0, 200) });
      } else if (child.nodeType === Node.ELEMENT_NODE) {
        const childNode = walk(child);
        if (childNode) children.push(childNode);
      }
    }

    // Build node descriptor
    const node = {};
    if (role) node.role = role;
    if (name) node.name = name;
    if (ref) node.ref = ref;
    if (children.length) node.children = children;

    // Collapse non-semantic wrapper with a single child (no ref, no role)
    if (!role && !ref && children.length === 1) {
      return children[0];
    }

    // Skip completely empty non-semantic, non-ref nodes
    if (!role && !ref && !children.length) {
      return null;
    }

    return node;
  }

  const tree = walk(document.body);
  return { tree, url: location.href, title: document.title };
}

// ─── Element lookup by ref ─────────────────────────────────────────────────

function findByRef(ref) {
  if (!ref) throw new Error('ref is required');
  const el = document.querySelector(`[data-cp-ref="${CSS.escape(ref)}"]`);
  if (!el) throw new Error(`Element not found for ref: ${ref}`);
  return el;
}

// ─── DOM command handlers ──────────────────────────────────────────────────

function domClick(params) {
  const el = findByRef(params.ref);
  if (params.double) {
    el.dispatchEvent(new MouseEvent('dblclick', { bubbles: true, cancelable: true }));
  } else {
    el.click();
  }
  return { clicked: params.ref, double: !!params.double };
}

function domType(params) {
  const el = findByRef(params.ref);
  el.focus();

  const text = params.text || '';
  const slowly = params.slowly || params['--slowly'];
  const submit = params.submit || params['--submit'];

  if (slowly) {
    // Type character by character with keyboard events
    // First clear existing value
    const nativeInputValueSetter = Object.getOwnPropertyDescriptor(
      window.HTMLInputElement.prototype, 'value'
    );
    if (nativeInputValueSetter && nativeInputValueSetter.set) {
      nativeInputValueSetter.set.call(el, '');
    } else {
      el.value = '';
    }
    el.dispatchEvent(new Event('input', { bubbles: true }));

    for (const char of text) {
      el.dispatchEvent(new KeyboardEvent('keydown', { key: char, bubbles: true }));
      el.dispatchEvent(new KeyboardEvent('keypress', { key: char, bubbles: true }));

      // Append character to current value
      const setter = Object.getOwnPropertyDescriptor(
        window.HTMLInputElement.prototype, 'value'
      ) || Object.getOwnPropertyDescriptor(
        window.HTMLTextAreaElement.prototype, 'value'
      );
      if (setter && setter.set) {
        setter.set.call(el, el.value + char);
      } else {
        el.value = el.value + char;
      }
      el.dispatchEvent(new Event('input', { bubbles: true }));
      el.dispatchEvent(new KeyboardEvent('keyup', { key: char, bubbles: true }));
    }
  } else {
    // Use native value setter to trigger React/Vue synthetic events
    const proto = el instanceof HTMLTextAreaElement
      ? window.HTMLTextAreaElement.prototype
      : window.HTMLInputElement.prototype;
    const nativeSetter = Object.getOwnPropertyDescriptor(proto, 'value');
    if (nativeSetter && nativeSetter.set) {
      nativeSetter.set.call(el, text);
    } else {
      el.value = text;
    }
    el.dispatchEvent(new Event('input', { bubbles: true }));
    el.dispatchEvent(new Event('change', { bubbles: true }));
  }

  if (submit) {
    // Try form.submit() first, fallback to Enter key
    const form = el.closest('form');
    if (form) {
      form.submit();
    } else {
      el.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', code: 'Enter', keyCode: 13, bubbles: true }));
      el.dispatchEvent(new KeyboardEvent('keypress', { key: 'Enter', code: 'Enter', keyCode: 13, bubbles: true }));
      el.dispatchEvent(new KeyboardEvent('keyup', { key: 'Enter', code: 'Enter', keyCode: 13, bubbles: true }));
    }
  }

  return { typed: text, into: params.ref, slowly: !!slowly, submitted: !!submit };
}

function domHover(params) {
  const el = findByRef(params.ref);
  el.dispatchEvent(new MouseEvent('mouseenter', { bubbles: true, cancelable: true }));
  el.dispatchEvent(new MouseEvent('mouseover', { bubbles: true, cancelable: true }));
  return { hovered: params.ref };
}

function domKey(params) {
  const target = document.activeElement || document.body;
  const key = params.key || '';
  const eventInit = {
    key,
    code: params.code || key,
    keyCode: params.keyCode || 0,
    bubbles: true,
    cancelable: true,
    ctrlKey: !!params.ctrlKey,
    shiftKey: !!params.shiftKey,
    altKey: !!params.altKey,
    metaKey: !!params.metaKey,
  };
  target.dispatchEvent(new KeyboardEvent('keydown', eventInit));
  target.dispatchEvent(new KeyboardEvent('keypress', eventInit));
  target.dispatchEvent(new KeyboardEvent('keyup', eventInit));
  return { key, target: target.tagName };
}

function domSelect(params) {
  const el = findByRef(params.ref);
  if (el.tagName !== 'SELECT') throw new Error(`Element ${params.ref} is not a <select>`);

  const value = params.value;
  const label = params.label;

  let found = false;
  for (const option of el.options) {
    if (
      (value !== undefined && option.value === String(value)) ||
      (label !== undefined && option.text.trim() === String(label))
    ) {
      option.selected = true;
      found = true;
      break;
    }
  }

  if (!found) {
    throw new Error(`No option found for value="${value}" label="${label}" in ref ${params.ref}`);
  }

  el.dispatchEvent(new Event('change', { bubbles: true }));
  return { selected: value ?? label, ref: params.ref };
}

function domDrag(params) {
  const source = findByRef(params.sourceRef);
  const target = findByRef(params.targetRef);

  const sourceRect = source.getBoundingClientRect();
  const targetRect = target.getBoundingClientRect();

  const fromX = sourceRect.left + sourceRect.width / 2;
  const fromY = sourceRect.top + sourceRect.height / 2;
  const toX = targetRect.left + targetRect.width / 2;
  const toY = targetRect.top + targetRect.height / 2;

  const dataTransfer = new DataTransfer();

  source.dispatchEvent(new DragEvent('dragstart', { bubbles: true, cancelable: true, dataTransfer, clientX: fromX, clientY: fromY }));
  target.dispatchEvent(new DragEvent('dragenter', { bubbles: true, cancelable: true, dataTransfer, clientX: toX, clientY: toY }));
  target.dispatchEvent(new DragEvent('dragover', { bubbles: true, cancelable: true, dataTransfer, clientX: toX, clientY: toY }));
  target.dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer, clientX: toX, clientY: toY }));
  source.dispatchEvent(new DragEvent('dragend', { bubbles: true, cancelable: true, dataTransfer, clientX: toX, clientY: toY }));

  return { dragged: params.sourceRef, onto: params.targetRef };
}

function domEval(params) {
  let el = null;
  if (params.ref) {
    el = findByRef(params.ref);
  }
  // new Function scope: receives (element, params)
  // eslint-disable-next-line no-new-func
  const fn = new Function('element', 'params', params.expression);
  const result = fn(el, params);
  return { result };
}

// ─── Page command handlers ─────────────────────────────────────────────────

function pageContent(params) {
  const format = params.format || 'html';
  if (format === 'text') {
    return { content: document.body.innerText };
  }
  return { content: document.documentElement.outerHTML };
}

async function pageWait(params) {
  const timeout = (params.time || 10) * 1000;
  const interval = 200;
  const deadline = Date.now() + timeout;

  // Plain time delay (no text conditions)
  if (params.time && !params.text && !params.textGone) {
    await new Promise((r) => setTimeout(r, params.time * 1000));
    return { result: { success: true } };
  }

  while (Date.now() < deadline) {
    const bodyText = document.body.innerText;
    if (params.text && bodyText.includes(params.text)) {
      return { result: { found: true } };
    }
    if (params.textGone && !bodyText.includes(params.textGone)) {
      return { result: { gone: true } };
    }
    await new Promise((r) => setTimeout(r, interval));
  }

  if (params.text) return { error: 'timeout: text not found' };
  if (params.textGone) return { error: 'timeout: text still present' };
  return { result: { success: true } };
}

// ─── Main entry point ──────────────────────────────────────────────────────

/**
 * executeInPage(method, params)
 *
 * Routes a command to the appropriate handler.
 * Returns a plain object; async handlers must be awaited by the caller.
 *
 * @param {string} method   - e.g. 'snapshot', 'dom.click', 'page.wait'
 * @param {object} params   - command-specific parameters
 * @returns {Promise<object>}
 */
async function executeInPage(method, params = {}) {
  try {
    switch (method) {
      case 'snapshot':
      case 'page.snapshot':
        return { result: buildSnapshot() };

      case 'dom.click':
        return { result: domClick(params) };

      case 'dom.type':
        return { result: domType(params) };

      case 'dom.hover':
        return { result: domHover(params) };

      case 'dom.key':
        return { result: domKey(params) };

      case 'dom.select':
        return { result: domSelect(params) };

      case 'dom.drag':
        return { result: domDrag(params) };

      case 'dom.eval':
        return { result: domEval(params) };

      case 'page.content':
        return { result: pageContent(params) };

      case 'page.wait':
        return await pageWait(params);

      default:
        return { error: `Unknown method: ${method}` };
    }
  } catch (err) {
    return { error: err.message ?? String(err) };
  }
}
