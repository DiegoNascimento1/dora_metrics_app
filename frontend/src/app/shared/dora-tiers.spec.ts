import {
  DEFAULT_THRESHOLDS,
  TIER_RANK,
  classifyMetric,
  cutoffsFor,
  formatDelta,
  formatMetricValue,
  nextTierProgress,
  worstTier,
} from './dora-tiers';

describe('dora-tiers helpers', () => {
  // ---------- classifyMetric ----------

  describe('classifyMetric', () => {
    it('returns insufficient_data when value is null/undefined', () => {
      expect(classifyMetric('df', null)).toBe('insufficient_data');
      expect(classifyMetric('lt', undefined)).toBe('insufficient_data');
    });

    it('classifies DF tiers correctly (higher is better)', () => {
      expect(classifyMetric('df', 5.0)).toBe('elite');
      expect(classifyMetric('df', 0.5)).toBe('high');
      expect(classifyMetric('df', 0.05)).toBe('medium');
      expect(classifyMetric('df', 0.001)).toBe('low');
      expect(classifyMetric('df', 0)).toBe('insufficient_data');
    });

    it('classifies LT tiers correctly (lower is better)', () => {
      expect(classifyMetric('lt', 1800)).toBe('elite');       // 30 min
      expect(classifyMetric('lt', 3 * 86400)).toBe('high');   // 3 dias
      expect(classifyMetric('lt', 10 * 86400)).toBe('medium'); // 10 dias
      expect(classifyMetric('lt', 60 * 86400)).toBe('low');   // 60 dias
    });

    it('classifies CFR tiers correctly', () => {
      expect(classifyMetric('cfr', 0.02)).toBe('elite');
      expect(classifyMetric('cfr', 0.08)).toBe('high');
      expect(classifyMetric('cfr', 0.15)).toBe('medium');
      expect(classifyMetric('cfr', 0.30)).toBe('low');
    });
  });

  // ---------- worstTier ----------

  describe('worstTier', () => {
    it('returns the lowest-ranked classification', () => {
      expect(worstTier(['elite', 'high', 'medium'])).toBe('medium');
      expect(worstTier(['elite', 'low'])).toBe('low');
    });

    it('ignores insufficient_data when there are real tiers', () => {
      expect(worstTier(['insufficient_data', 'high', 'medium'])).toBe('medium');
    });

    it('returns insufficient_data when all are insufficient', () => {
      expect(worstTier(['insufficient_data', 'insufficient_data'])).toBe('insufficient_data');
    });
  });

  // ---------- nextTierProgress ----------

  describe('nextTierProgress', () => {
    it('says "Você está no topo" for elite metric', () => {
      const p = nextTierProgress('df', 5.0); // elite DF
      expect(p.current).toBe('elite');
      expect(p.next).toBeNull();
      expect(p.label).toContain('topo');
    });

    it('returns delta and label pointing to next tier for non-elite', () => {
      // Medium DF (~0.05): próximo = high (>= 1/7 ≈ 0.143).
      const p = nextTierProgress('df', 0.05);
      expect(p.current).toBe('medium');
      expect(p.next).toBe('high');
      expect(p.delta).toBeGreaterThan(0);
      expect(p.label).toMatch(/para High/i);
    });

    it('handles insufficient data gracefully', () => {
      const p = nextTierProgress('df', null);
      expect(p.current).toBe('insufficient_data');
      expect(p.next).toBeNull();
      expect(p.progress).toBe(0);
    });

    it('clamps progress to [0,1] interval', () => {
      const p = nextTierProgress('cfr', 0.07); // high
      expect(p.progress).toBeGreaterThanOrEqual(0);
      expect(p.progress).toBeLessThanOrEqual(1);
    });
  });

  // ---------- TIER_RANK ----------

  it('TIER_RANK has expected ordering', () => {
    expect(TIER_RANK.elite).toBeGreaterThan(TIER_RANK.high);
    expect(TIER_RANK.high).toBeGreaterThan(TIER_RANK.medium);
    expect(TIER_RANK.medium).toBeGreaterThan(TIER_RANK.low);
    expect(TIER_RANK.low).toBeGreaterThan(TIER_RANK.insufficient_data);
  });

  // ---------- formatters ----------

  describe('formatMetricValue', () => {
    it('returns em-dash for null', () => {
      expect(formatMetricValue('df', null)).toBe('—');
    });
    it('formats DF as deploys/dia', () => {
      expect(formatMetricValue('df', 1.23)).toMatch(/1\.23/);
    });
    it('formats CFR as percentage', () => {
      expect(formatMetricValue('cfr', 0.123)).toContain('%');
    });
    it('formats LT durations in human form', () => {
      expect(formatMetricValue('lt', 30)).toBe('30s');
      expect(formatMetricValue('lt', 3600)).toBe('1.0h');
      expect(formatMetricValue('lt', 86400)).toBe('1.0d');
    });
  });

  describe('formatDelta', () => {
    it('returns 0 for zero delta', () => {
      expect(formatDelta('df', 0)).toBe('0');
    });
    it('formats CFR delta in percentage points', () => {
      expect(formatDelta('cfr', 0.05)).toContain('pp');
    });
  });

  describe('cutoffsFor', () => {
    it('returns 4 tier cutoffs for any metric', () => {
      const c = cutoffsFor('df', DEFAULT_THRESHOLDS);
      expect(c.low).toBeDefined();
      expect(c.medium).toBeDefined();
      expect(c.high).toBeDefined();
      expect(c.elite).toBeDefined();
    });
  });
});
