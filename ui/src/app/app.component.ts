import { Component, OnInit } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { CoreModule } from '@onega-ui/core';
import { KitModule } from '@onega-ui/kit';
import { ConfigService } from './services/config.service';
import { ConfigName } from './services/models';

@Component({
  selector: 'app-root',
  templateUrl: './app.component.html',
  styleUrl: './app.component.scss',
  standalone: true,
  imports: [CoreModule, KitModule, FormsModule],
})
export class AppComponent implements OnInit {
  step = '';

  _listConfig: ConfigName[] = [];
  _createConfig: ConfigName = new ConfigName();

  constructor(
    private readonly cs: ConfigService,
  ) {
  }

  ngOnInit(): void {
    this.load();
  }

  load():void {
    this.cs.list().subscribe(value => this._listConfig = value);
  }

  create():void {
    this.cs.create(this._createConfig).subscribe(() => this.load());
  }

  edit(id: number):void {
    console.log(id);
  }
}
