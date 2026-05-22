import { ComponentFixture, TestBed } from '@angular/core/testing';
import { ErrorStateComponent } from './error-state.component';

describe('ErrorStateComponent', () => {
  let fixture: ComponentFixture<ErrorStateComponent>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [ErrorStateComponent],
    }).compileComponents();
    fixture = TestBed.createComponent(ErrorStateComponent);
  });

  it('creates with required title', () => {
    fixture.componentRef.setInput('title', 'Erro de teste');
    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('Erro de teste');
  });

  it('renders description when provided', () => {
    fixture.componentRef.setInput('title', 'Título');
    fixture.componentRef.setInput('description', 'Descrição detalhada');
    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('Descrição detalhada');
  });

  it('does NOT render description when empty', () => {
    fixture.componentRef.setInput('title', 'Só título');
    fixture.detectChanges();
    const descEl = fixture.nativeElement.querySelector('.desc');
    expect(descEl).toBeNull();
  });

  it('renders detail in <details> when provided', () => {
    fixture.componentRef.setInput('title', 'X');
    fixture.componentRef.setInput('detail', 'stack trace bruta');
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelector('details')).toBeTruthy();
    expect(fixture.nativeElement.textContent).toContain('stack trace bruta');
  });

  it('exposes role="alert" with aria-live=polite for screen readers', () => {
    fixture.componentRef.setInput('title', 'X');
    fixture.detectChanges();
    const alertEl = fixture.nativeElement.querySelector('[role="alert"]');
    expect(alertEl).toBeTruthy();
    expect(alertEl.getAttribute('aria-live')).toBe('polite');
  });

  it('switches data-tone based on variant', () => {
    fixture.componentRef.setInput('title', 'X');
    fixture.componentRef.setInput('variant', 'forbidden');
    fixture.detectChanges();
    const alertEl = fixture.nativeElement.querySelector('[role="alert"]');
    expect(alertEl.getAttribute('data-tone')).toBe('danger');
  });

  it('not-found variant uses muted tone', () => {
    fixture.componentRef.setInput('title', 'X');
    fixture.componentRef.setInput('variant', 'not-found');
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelector('[data-tone="muted"]')).toBeTruthy();
  });
});
