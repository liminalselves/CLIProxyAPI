import {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent as ReactKeyboardEvent,
} from 'react';
import { createPortal } from 'react-dom';
import { useTranslation } from 'react-i18next';
import {
  IconCalendar,
  IconChevronDown,
  IconChevronLeft,
  IconChevronRight,
  IconX,
} from './icons';
import {
  addMonths,
  buildMonthCells,
  buildWeekdayLabels,
  DAY_MS,
  formatDay,
  localeWeekStart,
  parseDay,
  startOfMonth,
  utcMidnight,
} from './datePickerUtils';
import styles from './DatePickerInput.module.scss';

interface DatePickerInputProps {
  label?: string;
  /** ISO day string `yyyy-mm-dd`, or '' when unset. */
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
  id?: string;
}

const VIEWPORT_MARGIN = 8;
const DROPDOWN_OFFSET = 6;
const DROPDOWN_WIDTH = 288;
const DROPDOWN_Z_INDEX = 2010;

const clamp = (value: number, min: number, max: number) => Math.min(Math.max(value, min), max);

const resolveDropdownStyle = (element: HTMLElement): CSSProperties => {
  const rect = element.getBoundingClientRect();
  const viewportWidth = window.innerWidth;
  const viewportHeight = window.innerHeight;
  const width = Math.min(DROPDOWN_WIDTH, Math.max(0, viewportWidth - VIEWPORT_MARGIN * 2));
  const left = clamp(
    rect.left + rect.width / 2 - width / 2,
    VIEWPORT_MARGIN,
    Math.max(VIEWPORT_MARGIN, viewportWidth - width - VIEWPORT_MARGIN)
  );
  // Reserve room for the calendar (header + weekday row + 6 week rows + footer).
  const opensUp = viewportHeight - rect.bottom < 360 && rect.top > viewportHeight - rect.bottom;
  return {
    position: 'fixed',
    ...(opensUp
      ? { bottom: viewportHeight - rect.top + DROPDOWN_OFFSET }
      : { top: rect.bottom + DROPDOWN_OFFSET }),
    left,
    width,
    zIndex: DROPDOWN_Z_INDEX,
  };
};

export function DatePickerInput({ label, value, onChange, disabled = false, id }: DatePickerInputProps) {
  const { t, i18n } = useTranslation();
  const generatedId = useId();
  const inputId = id ?? generatedId;
  const [open, setOpen] = useState(false);
  const [viewMonth, setViewMonth] = useState(() => startOfMonth(Date.now()));
  const [highlight, setHighlight] = useState(() => utcMidnight(Date.now()));
  const [today] = useState(() => utcMidnight(Date.now()));
  const wrapRef = useRef<HTMLDivElement | null>(null);
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const dropdownRef = useRef<HTMLDivElement | null>(null);
  const rafRef = useRef<number | null>(null);
  const [dropdownStyle, setDropdownStyle] = useState<CSSProperties | null>(null);
  const isOpen = open && !disabled;

  const selected = parseDay(value);

  const locale = useMemo(() => {
    try {
      return new Intl.DateTimeFormat(i18n.language || undefined).resolvedOptions().locale;
    } catch {
      return undefined;
    }
  }, [i18n.language]);

  const weekStart = useMemo(() => localeWeekStart(locale), [locale]);

  const displayFormatter = useMemo(
    () => new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' }),
    [locale]
  );
  const monthFormatter = useMemo(
    () => new Intl.DateTimeFormat(locale, { month: 'long', year: 'numeric', timeZone: 'UTC' }),
    [locale]
  );
  const weekdayLabels = useMemo(
    () => buildWeekdayLabels(locale, weekStart),
    [locale, weekStart]
  );

  const cells = useMemo(() => buildMonthCells(viewMonth, weekStart), [viewMonth, weekStart]);

  useEffect(() => {
    if (!open || disabled) return;
    const handleClickOutside = (event: MouseEvent) => {
      const target = event.target as Node;
      if (wrapRef.current?.contains(target) || dropdownRef.current?.contains(target)) return;
      setOpen(false);
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, [disabled, open]);

  const updateDropdownStyle = useCallback(() => {
    if (!wrapRef.current) return;
    setDropdownStyle(resolveDropdownStyle(wrapRef.current));
  }, []);

  const scheduleDropdownStyleUpdate = useCallback(() => {
    if (typeof window === 'undefined') return;
    if (rafRef.current !== null) window.cancelAnimationFrame(rafRef.current);
    rafRef.current = window.requestAnimationFrame(() => {
      rafRef.current = null;
      updateDropdownStyle();
    });
  }, [updateDropdownStyle]);

  useLayoutEffect(() => {
    if (!isOpen) {
      if (rafRef.current !== null && typeof window !== 'undefined') {
        window.cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
      }
      return;
    }
    updateDropdownStyle();
    const handleViewportChange = () => scheduleDropdownStyleUpdate();
    window.addEventListener('resize', handleViewportChange);
    window.addEventListener('scroll', handleViewportChange, true);
    return () => {
      window.removeEventListener('resize', handleViewportChange);
      window.removeEventListener('scroll', handleViewportChange, true);
      if (rafRef.current !== null) {
        window.cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
      }
    };
  }, [isOpen, scheduleDropdownStyleUpdate, updateDropdownStyle]);

  const openCalendar = useCallback(() => {
    const initial = selected ?? today;
    setViewMonth(startOfMonth(initial));
    setHighlight(initial);
    setOpen(true);
  }, [selected, today]);

  const commit = useCallback(
    (time: number) => {
      onChange(formatDay(time));
      setOpen(false);
      triggerRef.current?.focus({ preventScroll: true });
    },
    [onChange]
  );

  const clear = useCallback(() => {
    onChange('');
    setOpen(false);
    triggerRef.current?.focus({ preventScroll: true });
  }, [onChange]);

  const syncViewToHighlight = (time: number) => {
    setViewMonth((prev) => {
      const d = new Date(time);
      return d.getUTCFullYear() * 12 + d.getUTCMonth() ===
        new Date(prev).getUTCFullYear() * 12 + new Date(prev).getUTCMonth()
        ? prev
        : startOfMonth(time);
    });
  };

  const moveHighlightDays = (days: number) => {
    const next = highlight + days * DAY_MS;
    setHighlight(next);
    syncViewToHighlight(next);
  };

  const moveHighlightMonths = (months: number) => {
    const d = new Date(highlight);
    const target = new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + months + 1, 0));
    const next = Date.UTC(
      target.getUTCFullYear(),
      target.getUTCMonth(),
      Math.min(d.getUTCDate(), target.getUTCDate())
    );
    setHighlight(next);
    syncViewToHighlight(next);
  };

  const handleKeyDown = (event: ReactKeyboardEvent<HTMLElement>) => {
    if (disabled) return;
    switch (event.key) {
      case 'ArrowDown':
        event.preventDefault();
        if (!isOpen) openCalendar();
        else moveHighlightDays(7);
        return;
      case 'ArrowUp':
        event.preventDefault();
        if (!isOpen) openCalendar();
        else moveHighlightDays(-7);
        return;
      case 'ArrowLeft':
        if (!isOpen) return;
        event.preventDefault();
        moveHighlightDays(-1);
        return;
      case 'ArrowRight':
        if (!isOpen) return;
        event.preventDefault();
        moveHighlightDays(1);
        return;
      case 'PageUp':
        if (!isOpen) return;
        event.preventDefault();
        moveHighlightMonths(event.shiftKey ? -12 : -1);
        return;
      case 'PageDown':
        if (!isOpen) return;
        event.preventDefault();
        moveHighlightMonths(event.shiftKey ? 12 : 1);
        return;
      case 'Enter':
      case ' ':
        event.preventDefault();
        if (!isOpen) openCalendar();
        else commit(highlight);
        return;
      case 'Escape':
        if (!isOpen) return;
        event.preventDefault();
        event.stopPropagation();
        setOpen(false);
        triggerRef.current?.focus({ preventScroll: true });
        return;
      case 'Tab':
        if (isOpen) setOpen(false);
        return;
      default:
        return;
    }
  };

  const displayText = selected !== null ? displayFormatter.format(new Date(selected)) : '';

  const dropdown =
    isOpen && dropdownStyle ? (
      <div
        ref={dropdownRef}
        className={styles.dropdown}
        style={dropdownStyle}
        role="grid"
        aria-label={label ?? t('common.select_date')}
      >
        <div className={styles.header}>
          <div className={styles.navGroup}>
            <button
              type="button"
              className={styles.navBtn}
              onClick={() => setViewMonth((prev) => addMonths(prev, -12))}
              aria-label={t('common.prev_year')}
            >
              <span className={styles.navDouble}>
                <IconChevronLeft size={14} />
                <IconChevronLeft size={14} />
              </span>
            </button>
            <button
              type="button"
              className={styles.navBtn}
              onClick={() => setViewMonth((prev) => addMonths(prev, -1))}
              aria-label={t('common.prev_month')}
            >
              <IconChevronLeft size={14} />
            </button>
          </div>
          <span className={styles.monthLabel}>{monthFormatter.format(new Date(viewMonth))}</span>
          <div className={styles.navGroup}>
            <button
              type="button"
              className={styles.navBtn}
              onClick={() => setViewMonth((prev) => addMonths(prev, 1))}
              aria-label={t('common.next_month')}
            >
              <IconChevronRight size={14} />
            </button>
            <button
              type="button"
              className={styles.navBtn}
              onClick={() => setViewMonth((prev) => addMonths(prev, 12))}
              aria-label={t('common.next_year')}
            >
              <span className={styles.navDouble}>
                <IconChevronRight size={14} />
                <IconChevronRight size={14} />
              </span>
            </button>
          </div>
        </div>
        <div className={styles.weekdays}>
          {weekdayLabels.map((name, index) => (
            <span key={`${name}-${index}`} className={styles.weekday} aria-hidden="true">
              {name}
            </span>
          ))}
        </div>
        <div className={styles.grid}>
          {cells.map((time) => {
            const d = new Date(time);
            const outside = d.getUTCMonth() !== new Date(viewMonth).getUTCMonth();
            const isSelected = selected !== null && time === selected;
            const isHighlighted = isOpen && time === highlight;
            const isToday = time === today;
            const classes = [
              styles.day,
              outside ? styles.dayOutside : '',
              isHighlighted ? styles.dayHighlighted : '',
              isToday ? styles.dayToday : '',
              isSelected ? styles.daySelected : '',
            ]
              .filter(Boolean)
              .join(' ');
            return (
              <button
                key={time}
                type="button"
                role="gridcell"
                className={classes}
                aria-selected={isSelected}
                aria-current={isToday ? 'date' : undefined}
                tabIndex={-1}
                onMouseEnter={() => setHighlight(time)}
                onClick={() => commit(time)}
              >
                {d.getUTCDate()}
              </button>
            );
          })}
        </div>
        <div className={styles.footer}>
          <button type="button" className={styles.footerBtn} onClick={() => commit(today)}>
            {t('common.today')}
          </button>
          <button type="button" className={styles.footerBtn} onClick={clear}>
            {t('common.clear')}
          </button>
        </div>
      </div>
    ) : null;

  return (
    <div className="form-group">
      {label && <label htmlFor={inputId}>{label}</label>}
      <div className={styles.wrap} ref={wrapRef}>
        <button
          id={inputId}
          ref={triggerRef}
          type="button"
          className={styles.trigger}
          onClick={disabled ? undefined : () => (isOpen ? setOpen(false) : openCalendar())}
          onKeyDown={handleKeyDown}
          aria-haspopup="dialog"
          aria-expanded={isOpen}
          disabled={disabled}
        >
          <span className={styles.calendarIcon} aria-hidden="true">
            <IconCalendar size={15} />
          </span>
          <span className={`${styles.triggerText} ${displayText ? '' : styles.placeholder}`.trim()}>
            {displayText || t('common.select_date')}
          </span>
          <span className={styles.triggerIcon} aria-hidden="true">
            <IconChevronDown size={14} />
          </span>
        </button>
        {value && !disabled && (
          <button
            type="button"
            className={styles.clear}
            aria-label={t('common.clear')}
            onClick={clear}
          >
            <IconX size={11} />
          </button>
        )}
      </div>
      {dropdown &&
        (typeof document === 'undefined' ? dropdown : createPortal(dropdown, document.body))}
    </div>
  );
}
