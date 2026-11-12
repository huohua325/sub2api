(origin) => {
  const auth = { id: 3, email: 'review@example.invalid', username: 'Review', role: 'user', status: 'active', balance: 0, concurrency: 5 };
  function resolve(path, method) {
    if (!path.startsWith('/api/v1/') || (method !== 'GET' && !(method === 'POST' && path === '/api/v1/usage/dashboard/api-keys-usage'))) {
      throw new Error('Blocked business request: ' + method + ' ' + path);
    }
    if (path.endsWith('/auth/me') || path.endsWith('/user/profile')) return auth;
    if (path.endsWith('/settings/public')) return {
      site_name: 'MILE Lab', run_mode: 'standard', api_base_url: origin, custom_endpoints: [],
      registration_enabled: false, subscription_enabled: false, payment_enabled: false, announcement_enabled: false,
    };
    if (path === '/api/v1/keys') return {
      items: [{ id: 1, user_id: 3, name: 'Review only', key: 'sk-review-invalid', status: 'active', group_id: 7,
        group: { id: 7, name: 'Codex', platform: 'openai', purpose: 'coding' }, quota: 0, quota_used: 0,
        created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' }],
      total: 1, page: 1, page_size: 20, pages: 1,
    };
    if (path.endsWith('/usage/dashboard/api-keys-usage')) return { stats: {} };
    if (path.includes('groups/available') || path.includes('subscriptions') || path.includes('announcements')) return [];
    if (path.includes('notifications')) return { items: [], unread_count: 0 };
    return {};
  }
  return { auth, resolve };
}
