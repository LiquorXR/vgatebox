"""Opt-in raw CSV mirror; all retrieval and validation execute locally."""
from dataclasses import dataclass
from datetime import datetime, timezone
import hashlib
import json
import re

from .common import ServiceError

PROVIDER = 'GeorgeXie2333/vpngate-list-mirror'
INDEX_URL = 'https://raw.githubusercontent.com/' + PROVIDER + '/main/latest.json'
_COMMIT_RE = re.compile('[a-f0-9]{40}')
_SHA_RE = re.compile('[a-f0-9]{64}')
_UPSTREAM = 'https://www.vpngate.net/api/iphone/'


def timestamp(value):
    if not isinstance(value, str):
        raise ValueError('timestamp')
    parsed = datetime.fromisoformat(value.replace('Z', '+00:00'))
    if parsed.tzinfo is None:
        raise ValueError('timezone')
    return parsed


@dataclass(frozen=True)
class _MirrorIndex:
    commit: str
    fetched: datetime
    size: int
    sha: str
    count: int


def _parse_index(raw):
    try:
        index = json.loads(raw)
        if type(index['schema_version']) is not int or index['schema_version'] != 1:
            raise ValueError('schema')
        commit = index['data_commit']
        if not isinstance(commit, str) or not _COMMIT_RE.fullmatch(commit):
            raise ValueError('commit')
        if index.get('source_url') != _UPSTREAM:
            raise ValueError('upstream')
        fetched = timestamp(index['fetched_at'])
        description = index['files']['data/vpngate.csv']
        size, sha = description['bytes'], description['sha256']
        if type(size) is not int or not 1 <= size <= 12_000_000:
            raise ValueError('size')
        if not isinstance(sha, str) or not _SHA_RE.fullmatch(sha):
            raise ValueError('hash')
        count = index['server_count']
        if type(count) is not int or not 1 <= count <= 100_000:
            raise ValueError('count')
        return _MirrorIndex(commit=commit, fetched=fetched, size=size, sha=sha, count=count)
    except (ValueError, TypeError, KeyError):
        raise ServiceError('mirror_index_invalid') from None


def _check_snapshot_age(fetched, max_age_hours, now=None):
    now = now or datetime.now(timezone.utc)
    age = (now - fetched).total_seconds()
    if age < -300:
        raise ServiceError('mirror_time_invalid')
    if age > max_age_hours * 3600:
        raise ServiceError('mirror_snapshot_expired')


def _check_snapshot_rollback(previous, fetched, sha):
    previous = previous or {}
    if previous.get('mirror_provider') != PROVIDER:
        return
    try:
        older = timestamp(previous.get('snapshot_fetched_at'))
    except ValueError:
        older = None
    if older is not None and fetched < older:
        raise ServiceError('mirror_snapshot_rollback')
    if older == fetched and previous.get('sha256') not in (None, sha):
        raise ServiceError('mirror_snapshot_conflict')


def _mirror_urls(commit):
    return ['https://raw.githubusercontent.com/' + PROVIDER + '/' + commit + '/data/vpngate.csv',
            'https://cdn.jsdelivr.net/gh/' + PROVIDER + '@' + commit + '/data/vpngate.csv']


def _download_snapshot(fetcher, timeout, urls, expected):
    for url in urls:
        try:
            data = fetcher.fetch(url, timeout, limit=expected.size)
        except ServiceError as exc:
            if str(exc) == 'response_too_large':
                raise ServiceError('mirror_integrity_failed') from None
            continue
        if len(data) != expected.size or hashlib.sha256(data).hexdigest() != expected.sha:
            raise ServiceError('mirror_integrity_failed')
        return data, url
    raise ServiceError('mirror_download_failed')


def fetch_mirror(fetcher, timeout, max_age_hours, previous=None, now=None):
    try:
        raw = fetcher.fetch(INDEX_URL, timeout, limit=65_536)
    except ServiceError as exc:
        code = 'mirror_index_invalid' if str(exc) == 'response_too_large' else 'mirror_index_unavailable'
        raise ServiceError(code) from None
    expected = _parse_index(raw)
    _check_snapshot_age(expected.fetched, max_age_hours, now)
    _check_snapshot_rollback(previous, expected.fetched, expected.sha)
    data, url = _download_snapshot(fetcher, timeout, _mirror_urls(expected.commit), expected)
    return data, {'name': url, 'kind': 'mirror', 'mirror_provider': PROVIDER,
                  'snapshot_fetched_at': expected.fetched.astimezone(timezone.utc).isoformat(),
                  'data_commit': expected.commit, 'integrity_verified': True,
                  'publisher_count': expected.count}
