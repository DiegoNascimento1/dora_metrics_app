import { TestBed } from '@angular/core/testing';
import { OnboardingService } from './onboarding-tour.component';

describe('OnboardingService', () => {
  let svc: OnboardingService;

  beforeEach(() => {
    localStorage.clear();
    TestBed.configureTestingModule({});
    svc = TestBed.inject(OnboardingService);
  });

  it('starts inactive', () => {
    expect(svc.active()).toBeFalse();
    expect(svc.current()).toBe(-1);
  });

  it('start() activates first step on initial visit', () => {
    svc.start(false);
    expect(svc.active()).toBeTrue();
    expect(svc.current()).toBe(0);
  });

  it('start(false) skips when already seen', () => {
    localStorage.setItem('dora.tour.seen', '1');
    svc.start(false);
    expect(svc.active()).toBeFalse();
  });

  it('start(true) forces tour even when previously seen', () => {
    localStorage.setItem('dora.tour.seen', '1');
    svc.start(true);
    expect(svc.active()).toBeTrue();
  });

  it('next() advances and finish() marks as seen', () => {
    svc.start(true);
    const total = svc.steps().length;
    for (let i = 0; i < total - 1; i++) {
      svc.next();
    }
    expect(svc.current()).toBe(total - 1);
    svc.next(); // last step → finish
    expect(svc.active()).toBeFalse();
    expect(localStorage.getItem('dora.tour.seen')).toBe('1');
  });

  it('prev() does not go below 0', () => {
    svc.start(true);
    svc.prev();
    svc.prev();
    expect(svc.current()).toBe(0);
  });

  it('reset() clears seen flag and restarts at step 0', () => {
    localStorage.setItem('dora.tour.seen', '1');
    svc.reset();
    expect(localStorage.getItem('dora.tour.seen')).toBeNull();
    expect(svc.current()).toBe(0);
  });

  it('hasSeen() reflects localStorage state', () => {
    expect(svc.hasSeen()).toBeFalse();
    localStorage.setItem('dora.tour.seen', '1');
    expect(svc.hasSeen()).toBeTrue();
  });
});
