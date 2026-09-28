// Reading and writing a rule's @mention list.
//
// The list lives in alert_rules.at_users as a JSON string and is resolved at
// send time by the backend's resolveAtUsers, which looks each name up in
// alert_contacts. That lookup runs in MySQL under utf8mb4_unicode_ci, so it is
// case-insensitive: a rule holding "bruce" reaches the contact named "Bruce"
// and has always done so. The selector has to match the same way, because the
// alternative — comparing with === in JavaScript — would fail to recognise the
// names a rule already has and quietly drop people the alert used to reach.
//
// Nothing here ever discards a name. A name with no contact behind it comes
// back marked unknown rather than filtered out: it is the one case where the
// person configuring the rule most needs to see what happened, and the old
// textarea showed it plainly.

/** The key both sides of a name comparison are reduced to. */
function key(name) {
  return String(name ?? '').trim().toLowerCase()
}

/**
 * parseAtUsers reads the stored JSON into a list of raw names.
 *
 * Two shapes are in the database. The current one is a plain array of names,
 * ["Bruce","Cesar"]. The older one carries the resolved id alongside,
 * [{"name":"Bruce","user_id":"ou_xxx"}], and the backend still accepts it, so
 * a rule saved years ago must round-trip through this selector unharmed.
 *
 * Returns null — not [] — when the string cannot be parsed. An empty list and
 * an unreadable one call for opposite responses: the first means "nobody is
 * mentioned", the second means "do not touch this until a person looks at it".
 * Collapsing them would let a malformed value be silently overwritten with [].
 */
export function parseAtUsers(raw) {
  const s = String(raw ?? '').trim()
  if (!s) return []

  let parsed
  try {
    parsed = JSON.parse(s)
  } catch {
    return null
  }
  if (!Array.isArray(parsed)) return null

  const names = []
  for (const item of parsed) {
    const name = typeof item === 'string' ? item : item && typeof item === 'object' ? item.name : ''
    const trimmed = String(name ?? '').trim()
    if (trimmed) names.push(trimmed)
  }
  return names
}

/**
 * resolveNames pairs each stored name with its contact.
 *
 * Order is the order the rule already had. Whoever wrote that list may have
 * meant something by it, and reordering it would also make every rule's diff
 * look like an edit nobody made. Newly picked names append at the end.
 *
 * Duplicates collapse — including two spellings of one contact, which the
 * backend would have resolved to the same person and mentioned twice.
 */
export function resolveNames(names, contacts) {
  const byKey = new Map()
  for (const c of contacts || []) {
    const k = key(c && c.name)
    // First wins: alert_contacts.name is UNIQUE, so a collision here means two
    // rows differing only in case, and the earlier one is what the backend's
    // own lookup would have returned.
    if (k && !byKey.has(k)) byKey.set(k, c)
  }

  const out = []
  const seen = new Set()
  for (const raw of names || []) {
    const k = key(raw)
    if (!k || seen.has(k)) continue
    seen.add(k)

    const contact = byKey.get(k)
    if (contact) {
      // Display and store the contact's own spelling, so the rule shows the
      // same name the 通知人管理 page does.
      out.push({ name: contact.name, known: true, disabled: Number(contact.status) === 0 })
    } else {
      out.push({ name: raw, known: false, disabled: false })
    }
  }
  return out
}

/** serialize writes the list back in the shape the backend reads. */
export function serialize(entries) {
  return JSON.stringify((entries || []).map(e => e.name))
}

/** sameName reports whether two names refer to one contact. */
export function sameName(a, b) {
  return key(a) === key(b) && key(a) !== ''
}

/** matchesQuery is the selector's search: substring, case-insensitive. */
export function matchesQuery(name, query) {
  const q = key(query)
  return q === '' || key(name).includes(q)
}
